import assert from "node:assert/strict";
import { describe, it } from "node:test";
import {
  aggregate,
  exitCodeForStatus,
  type FlowResult
} from "../../src/status.js";

const flow = (status: FlowResult["status"], required = true): FlowResult => ({
  flowId: "fixture",
  required,
  status
});

describe("aggregate", () => {
  it("returns INVALID when configuration cannot be trusted", () => {
    assert.equal(aggregate([flow("FAIL")], { invalid: true }), "INVALID");
    assert.equal(aggregate([flow("PASS")], { invalid: true }), "INVALID");
  });

  it("fails when a required flow fails", () => {
    assert.equal(aggregate([flow("PASS"), flow("FAIL")]), "FAIL");
  });

  it("fails when a required flow is flaky after its single retry", () => {
    assert.equal(aggregate([flow("PASS"), flow("FLAKY")]), "FAIL");
  });

  it("fails before blocking when required failure and blocked coexist", () => {
    assert.equal(aggregate([flow("FAIL"), flow("BLOCKED")]), "FAIL");
  });

  it("blocks when a required flow is blocked without failure", () => {
    assert.equal(aggregate([flow("PASS"), flow("BLOCKED")]), "BLOCKED");
  });

  it("passes when every required flow passes", () => {
    assert.equal(aggregate([flow("PASS"), flow("PASS")]), "PASS");
  });

  it("reports NOT_APPLICABLE when no required flow applies", () => {
    assert.equal(aggregate([flow("NOT_APPLICABLE")]), "NOT_APPLICABLE");
    assert.equal(aggregate([]), "NOT_APPLICABLE");
  });

  it("passes when applicable required flows pass alongside not-applicable flows", () => {
    assert.equal(aggregate([flow("PASS"), flow("NOT_APPLICABLE")]), "PASS");
  });

  it("keeps optional SKIP and BLOCKED visible without changing final status", () => {
    assert.equal(
      aggregate([flow("PASS"), flow("SKIP", false), flow("BLOCKED", false)]),
      "PASS"
    );
  });

  it("blocks rather than passing when a required flow was skipped", () => {
    assert.equal(aggregate([flow("SKIP")]), "BLOCKED");
  });
});

describe("exitCodeForStatus", () => {
  it("maps statuses to contract exit codes", () => {
    assert.equal(exitCodeForStatus("PASS"), 0);
    assert.equal(exitCodeForStatus("NOT_APPLICABLE"), 0);
    assert.equal(exitCodeForStatus("FAIL"), 1);
    assert.equal(exitCodeForStatus("BLOCKED"), 2);
    assert.equal(exitCodeForStatus("INVALID"), 3);
  });
});
