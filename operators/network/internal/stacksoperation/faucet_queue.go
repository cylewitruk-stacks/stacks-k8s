package stacksoperation

import (
	"context"
	"sort"
	"time"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/faucetrequest"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// faucetNotice is routing metadata, never permission to send a transfer.
type faucetNotice struct {
	key              client.ObjectKey
	uid              types.UID
	created, expires time.Time
	deleted          bool
}

// CollectionWatches selects the request collection in this worker's namespace.
func (r *FaucetRole) CollectionWatches() []stacksworker.CollectionWatch {
	return []stacksworker.CollectionWatch{{APIVersion: stacks.GroupVersion.String(), Resource: "stacksfaucetrequests"}}
}

// CollectionChanged records bounded hints from initial lists, relists and duplicate events.
func (r *FaucetRole) CollectionChanged(ref stacksworker.CollectionWatch, object *unstructured.Unstructured, deleted bool) {
	if ref.APIVersion != stacks.GroupVersion.String() || ref.Resource != "stacksfaucetrequests" || object.GetNamespace() != r.Namespace {
		return
	}
	var request stacks.StacksFaucetRequest
	if runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &request) != nil {
		return
	}
	a := request.Status.Admission
	if a == nil || a.Decision != stacks.FaucetDecisionAdmitted || a.Faucet == nil || a.Worker == nil || a.Faucet.UID != r.ParticipantUID || a.Worker.UID != r.PodUID {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.notices == nil {
		r.notices = map[types.UID]faucetNotice{}
	}
	if _, active := r.entries[request.UID]; active {
		return
	}
	if faucetrequest.MatchingExecution(&request) && faucetrequest.TerminalExecution(request.Status.Execution) {
		delete(r.notices, request.UID)
		return
	}
	if _, exists := r.notices[request.UID]; !exists && len(r.notices)+len(r.entries) >= faucetrequest.Capacity {
		r.rescan = true
		return
	}
	expiry, _ := faucetrequest.Deadline(&request)
	r.notices[request.UID] = faucetNotice{key: client.ObjectKeyFromObject(&request), uid: request.UID, created: request.CreationTimestamp.Time, expires: expiry, deleted: deleted}
}

// takeNotice selects best-effort creation/UID order; pending sends do not block deadline refusals.
func (r *FaucetRole) takeNotice(pending bool) (faucetNotice, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	notices := make([]faucetNotice, 0, len(r.notices))
	for uid, notice := range r.notices {
		if _, active := r.entries[uid]; active {
			delete(r.notices, uid)
			continue
		}
		if !pending || notice.deleted || !r.now().Before(notice.expires) {
			notices = append(notices, notice)
		}
	}
	sort.Slice(notices, func(i, j int) bool {
		if !notices[i].created.Equal(notices[j].created) {
			return notices[i].created.Before(notices[j].created)
		}
		return notices[i].uid < notices[j].uid
	})
	if len(notices) == 0 {
		return faucetNotice{}, false
	}
	selected := notices[0]
	return selected, true
}

// rescanNotices repairs a bounded notification overflow only after capacity becomes available.
func (r *FaucetRole) rescanNotices(ctx context.Context) error {
	r.mu.Lock()
	needed := r.rescan && len(r.notices)+len(r.entries) < faucetrequest.Capacity/2
	if needed {
		r.rescan = false
	}
	r.mu.Unlock()
	if !needed {
		return nil
	}
	continuation := ""
	for {
		var list stacks.StacksFaucetRequestList
		if err := r.Client.List(ctx, &list, &client.ListOptions{Namespace: r.Namespace, Limit: 500, Continue: continuation}); err != nil {
			r.mu.Lock()
			r.rescan = true
			r.mu.Unlock()
			return err
		}
		for i := range list.Items {
			object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&list.Items[i])
			if err != nil {
				return err
			}
			r.CollectionChanged(r.CollectionWatches()[0], &unstructured.Unstructured{Object: object}, false)
		}
		continuation = list.Continue
		if continuation == "" {
			return nil
		}
	}
}

// forgetNotice discards a routing hint after a definitive fresh eligibility read.
func (r *FaucetRole) forgetNotice(uid types.UID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.notices, uid)
}

// putEntry converts a queued UID into retained local state without creating another capacity slot.
func (r *FaucetRole) putEntry(entry *faucetEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.notices, entry.request.UID)
	r.entries[entry.request.UID] = entry
}

// forgetEntry releases only settled and acknowledged local execution state.
func (r *FaucetRole) forgetEntry(uid types.UID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, uid)
}
