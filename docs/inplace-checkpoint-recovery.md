# Additive checkpoint recovery (experimental)

A restored Bor branch can lie below retained Heimdall checkpoint/milestone tips.
This explicit SDK message reconciles those cursors with a finalized, zero-reward
RootChain owner anchor. Ordinary checkpoint, ACK and proposer behavior is unchanged.
It does not restore missing historical Bor blocks or reconcile bridge liabilities.

The main handler checks the current validator sender, chain, previous counters and
checkpoint/milestone identities. Side voting verifies a successful canonical parent
receipt, the RootChain event and present header, and the local Bor range root/end
block hash. The post handler requires quorum approval, rechecks state and atomically
appends the checkpoint, milestone and a recovery journal containing the old buffer.
Previously retained checkpoint/milestone entries, auth, staking, span and state-sync
stores are not rewritten. It does not rotate rewarded-checkpoint proposer priority.

`heimdallcli tx checkpoint checkpoint-recovery message.json` uses a plain JSON file
matching `MsgCheckpointRecovery`. Use the actual finalized owner transaction hash
and current counters. Every consensus participant must understand the new message
before it is broadcast. Do not apply an owner anchor separately from the complete,
verified recovery sequence.

## Verification

Run `go test -p 2 -parallel 1 ./checkpoint/... ./app ./auth ./auth/types ./staking ./types`.
Tests reject unauthorized senders, stale counters, replay, failed/noncanonical parent
receipts, normal rewarded events, mismatched RootChain state and incorrect Bor roots
or hashes. They verify that side rejection writes nothing and old records remain.
Serial test execution avoids the existing global auth codec used by independent
parallel test app constructors.

An opt-in test (`TestRecoveryApplicationCopy`) only opens a second isolated Ravi
application DB copy at `/srv/ramestta-recovery/rehearsal/inplace-audit`.
Set `RAMESTTA_COPY_AUDIT=1` and `RAMESTTA_COPY_AUDIT_HOME` to that exact path. Add
`RAMESTTA_COPY_AUDIT_APPLY=1` to exercise side/post handlers against isolated parent
Anvil port 18549 and copied Bor port 18545. It never commits, exports/imports genesis,
signs or broadcasts. Every non-checkpoint store and previously retained history is
compared byte for byte. This does not simulate native consensus on the original DB.

A separate three-validator synthetic consensus rehearsal committed this message,
retained the previous checkpoint/milestone, and produced matching block/app hashes
on all participants. A subsequent ordinary signed parent checkpoint and native
ACK also committed after conditional governance epoch reconciliation. Those
synthetic identities and genesis are not production state.

## Production gates still open

- RootChain/Governance owner authority is a 3-of-5 multisig; fork impersonation is
  not production signing access.
- Owner anchors bypass normal StakeManager epoch advancement; the epoch/timeline
  invariant needs a separately validated governance operation.
- Nonmonotonic checkpoint ranges require segmented bridge proof routing. The
  deployed WithdrawManager regular-exit queue rejects repeated low-128-bit exit
  identities even when checkpoint timestamps differ (`KNOWN_EXIT`, tested on an
  isolated fork through the authorized-predicate boundary). Receipt inclusion,
  nonregular exits and full withdrawal execution are separate validation gates.
- Native Bor sealing/state-sync and a complete bridge withdrawal round trip remain
  unverified. Recovery journals also require explicit support in any future genesis
  migration that must preserve them.

Do not treat these tests as production launch approval or overwrite an existing
RootChain header or database to bypass the remaining gates.
