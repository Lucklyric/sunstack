package main

// v0.12.12: --op is refused for another host, where it could not stop a
// retry from delivering twice.

import "testing"

func TestRemoteSendRefusesOp(t *testing.T) {
	p := fixture(t)
	r := sh(t, p, isolatedEnv(t), "send", "laptop:demo/builder.alice", "hello", "--op", "retry-1")
	expect(t, r, 2, "remote send with --op")
	requireContains(t, r.stderr, "--op is for messages within this host")
}
