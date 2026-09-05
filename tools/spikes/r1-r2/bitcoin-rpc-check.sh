#!/bin/sh
# Check bounded RPC timeout behavior on a disposable, network-isolated regtest node.
set -eu

image=${BITCOIN_PROBE_IMAGE:-bitcoin/bitcoin:31.1}
docker image inspect "$image" --format 'image={{.Id}} platform={{.Os}}/{{.Architecture}}'
docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --memory 256m --cpus 1 \
  --tmpfs /tmp:rw,nosuid,size=128m --entrypoint /bin/sh "$image" -eu -c '
  mkdir /tmp/r1r2-bitcoin
  rpc() { bitcoin-cli -regtest -datadir=/tmp/r1r2-bitcoin "$@"; }
  trap "rpc stop >/dev/null 2>&1 || true" EXIT
  version=$(bitcoind -version -nosettings)
  printf "%s\n" "$version" | head -n 1
  bitcoind -regtest -datadir=/tmp/r1r2-bitcoin -daemon -server \
    -listen=0 -dnsseed=0 -discover=0 -rpcthreads=1 -rpcworkqueue=4 -dbcache=8
  rpc -rpcwait -rpcwaittimeout=10 getblockcount >/dev/null
  rpc createwallet r1r2 >/dev/null
  address=$(rpc getnewaddress)
  printf "peers="
  rpc getconnectioncount
  printf "height_before="
  rpc getblockcount
  rpc waitfornewblock 4000 >/tmp/wait-result &
  waiting_pid=$!
  sleep 0.3
  if rpc -rpcclienttimeout=1 generatetoaddress 1 "$address" >/tmp/mine-result 2>/tmp/mine-error; then
    echo "FAIL: generation response arrived before the client deadline"
    exit 1
  fi
  echo "client_result:"
  cat /tmp/mine-error
  grep -qi timeout /tmp/mine-error
  wait "$waiting_pid"
  height=$(rpc getblockcount)
  printf "height_after=%s\n" "$height"
  test "$height" = 1
  echo "PASS: one queued generation completed after client timeout with zero peers"
'
