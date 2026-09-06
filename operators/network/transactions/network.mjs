import { STACKS_TESTNET } from "@stacks/network";

/** Bind SDK calls to the selected test endpoint with bounded, nonredirecting requests. */
export function localNetwork(baseUrl) {
  return {
    ...STACKS_TESTNET,
    client: {
      baseUrl,
      fetch: (input, init) => fetch(input, {
        ...init,
        redirect: "error",
        signal: AbortSignal.timeout(10000),
      }),
    },
  };
}
