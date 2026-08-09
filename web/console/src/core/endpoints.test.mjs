import assert from "node:assert/strict";
import test from "node:test";

import { rootEntryEndpoint } from "./endpoints.js";

test("root entry accepts a sole endpoint while its health check is pending", () => {
  const endpoint = {
    endpoint_ref: "ep_remote",
    connected: false,
    health_pending: true,
  };

  assert.equal(rootEntryEndpoint([endpoint]), endpoint);
});

test("root entry rejects disconnected and ambiguous endpoints", () => {
  assert.equal(
    rootEntryEndpoint([
      {
        endpoint_ref: "ep_remote",
        connected: false,
        health_pending: false,
      },
    ]),
    null
  );
  assert.equal(
    rootEntryEndpoint([
      { endpoint_ref: "ep_one", connected: true },
      { endpoint_ref: "ep_two", connected: true },
    ]),
    null
  );
});
