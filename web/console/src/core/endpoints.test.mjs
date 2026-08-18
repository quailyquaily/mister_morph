import assert from "node:assert/strict";
import test from "node:test";

import { isEndpointSelectable, rootEntryEndpoint } from "./endpoints.js";

test("connected and health-pending endpoints are selectable", () => {
  assert.equal(isEndpointSelectable({ endpoint_ref: "ep_connected", connected: true }), true);
  assert.equal(
    isEndpointSelectable({ endpoint_ref: "ep_pending", connected: false, health_pending: true }),
    true,
  );
  assert.equal(
    isEndpointSelectable({ endpoint_ref: "ep_offline", connected: false, health_pending: false }),
    false,
  );
});

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
