"""Pure stdlib tests; no Redis, network, Docker, or benchmark resources."""

from dataclasses import replace
import unittest

import model


def event(state, name):
    matches = [next_state for action, next_state in model.transitions(state) if action == name]
    if len(matches) != 1:
        raise AssertionError(f"expected one enabled {name!r}, got {len(matches)}")
    return matches[0]


def sealed_state():
    state = model.State()
    for i in model.PARTICIPANTS:
        state = event(state, f"prepare P{i}")
        state = event(state, f"seal P{i}")
    return state


class ModelTests(unittest.TestCase):
    def test_repaired_exhaustive_finite_state_space(self):
        result = model.explore()
        self.assertIsNone(result.problem)
        self.assertIsNone(result.trace)
        self.assertEqual(result.states, 3096)
        self.assertEqual(result.edges, 14340)

    def test_bound_exhaustion_cannot_report_success(self):
        with self.assertRaisesRegex(RuntimeError, "INCOMPLETE"):
            model.explore(max_states=10)

    def test_naive_lease_gc_has_visible_missing_participant(self):
        result = model.explore(unsafe_lease_gc=True)
        self.assertIn("missing sealed participants", result.problem)
        self.assertTrue(any("UNSAFE lease-expiry deletion" in step for step in result.trace))
        self.assertIn("central commit", result.trace)

    def test_sealed_outputs_survive_expiry_and_publish_together(self):
        state = sealed_state()
        for i in model.PARTICIPANTS:
            state = event(state, f"expire lease P{i}")
        self.assertFalse(model.gc_allowed(state))
        state = event(state, "central commit")
        self.assertEqual(model.read_snapshot(state), (0, 1))

    def test_lost_seal_and_commit_replies_resolve_without_replaying_writes(self):
        state = event(model.State(), "prepare P0")
        state = event(state, "seal P0; reply lost")
        self.assertFalse(state.parts[0].receipt)
        self.assertNotIn("central commit", dict(model.transitions(state)))
        state = event(state, "read seal outcome P0")
        state = event(state, "prepare P1")
        state = event(state, "seal P1")
        state = event(state, "central commit; reply lost")
        self.assertEqual(state.known, "unknown")
        self.assertEqual(model.read_snapshot(state), (0, 1))
        # Cancellation after commit cannot decide abort, even with a lost reply.
        self.assertFalse(any("central abort" in action for action, _ in model.transitions(state)))
        state = event(state, "read decision after loss/cancel")
        self.assertEqual(state.known, "commit")

    def test_abort_is_terminal_and_authorizes_cleanup_with_live_leases(self):
        state = event(sealed_state(), "cancel: central abort; reply lost")
        self.assertTrue(model.gc_allowed(state))
        self.assertEqual(state.known, "unknown")
        self.assertTrue(all(p.lease for p in state.parts))
        for _, next_state in model.transitions(state):
            self.assertEqual(next_state.decision, "abort")
        state = event(state, "issue terminal GC ticket P0")
        state = event(state, "deliver GC ticket; fence P0")
        self.assertNotIn("seal P0", dict(model.transitions(state)))
        state = event(state, "finish local deletion P0")
        self.assertEqual(state.parts[0].phase, "deleted")

    def test_delayed_local_prepare_and_seal_after_abort_cannot_publish(self):
        state = event(model.State(), "cancel: central abort")
        state = event(state, "prepare P0")
        state = event(state, "seal P0")
        self.assertEqual(state.parts[0].phase, "sealed")
        self.assertTrue(state.parts[0].receipt)
        self.assertNotIn("central commit", dict(model.transitions(state)))
        state = event(state, "issue terminal GC ticket P0")
        state = event(state, "deliver GC ticket; fence P0")
        state = event(state, "finish local deletion P0")
        self.assertNotIn("prepare P0", dict(model.transitions(state)))
        self.assertNotIn("seal P0", dict(model.transitions(state)))

    def test_ticket_before_delayed_initialization_prevents_recreation(self):
        state = event(model.State(), "retire generation")
        state = event(state, "issue terminal GC ticket P0")
        self.assertEqual(state.parts[0].phase, "absent")
        state = event(state, "deliver GC ticket; fence P0")
        state = event(state, "finish local deletion P0")
        self.assertNotIn("prepare P0", dict(model.transitions(state)))
        # A different unfenced participant may still receive an earlier grant.
        state = event(state, "prepare P1")
        state = event(state, "seal P1; reply lost")
        state = event(state, "read seal outcome P1")
        self.assertNotIn("central commit", dict(model.transitions(state)))

    def test_retirement_blocks_commit_and_new_pins(self):
        retired_open = event(sealed_state(), "retire generation")
        self.assertNotIn("central commit", dict(model.transitions(retired_open)))
        retired_commit = event(event(sealed_state(), "central commit"), "retire generation")
        self.assertNotIn("pin committed generation", dict(model.transitions(retired_commit)))

    def test_retired_pin_prevents_tickets_until_release(self):
        state = event(event(sealed_state(), "central commit"), "pin committed generation")
        state = event(state, "retire generation")
        self.assertFalse(model.gc_allowed(state))
        self.assertFalse(any("GC ticket" in action for action, _ in model.transitions(state)))
        self.assertEqual(model.read_snapshot(state), (0, 1))
        state = event(state, "release reader pin")
        state = event(state, "issue terminal GC ticket P0")
        state = event(state, "deliver GC ticket; fence P0")
        state = event(state, "finish local deletion P0")
        self.assertIsNone(model.violation(state))
        with self.assertRaises(model.Unavailable):
            model.read_snapshot(state)

    def test_local_delete_checks_issued_exact_generation_participant_and_prefix(self):
        state = event(event(sealed_state(), "cancel: central abort"), "issue terminal GC ticket P0")
        valid = state.tickets[0]
        for invalid in (replace(valid, generation="old"), replace(valid, participant=1),
                        replace(valid, prefix="other/")):
            self.assertIsNone(model.authorize_delete(state, 0, invalid))
        self.assertIsNone(model.authorize_delete(sealed_state(), 0, valid))
        fenced = model.authorize_delete(state, 0, valid)
        self.assertEqual(fenced.parts[0].phase, "deleting")
        self.assertIsNone(model.authorize_delete(fenced, 0, valid))  # Duplicate delivery.

    def test_delayed_ticket_and_reused_prefix_counterexample_and_repairs(self):
        self.assertIn("new participant deleted", model.prefix_reuse_counterexample()[-1])
        ticket = model.Ticket("old", 0, "same/")
        replacement = model.LocalObject("new", 0, "same/")
        self.assertFalse(model.apply_ticket(replacement, ticket, generation_check=False).present)
        self.assertTrue(model.apply_ticket(replacement, ticket).present)
        with self.assertRaisesRegex(ValueError, "never be reused"):
            model.allocate({"same/"}, "new", 0, "same/")
        fresh = model.allocate({"same/"}, "new", 0, "new/participant-0/")
        self.assertTrue(model.apply_ticket(fresh, ticket).present)

    def test_sequential_allocations_record_and_never_reuse_prefix(self):
        used = set()
        model.allocate(used, "old", 0, "first/")
        self.assertEqual(used, {"first/"})
        with self.assertRaisesRegex(ValueError, "never be reused"):
            model.allocate(used, "new", 0, "first/")
        model.allocate(used, "new", 0, "second/")
        self.assertEqual(used, {"first/", "second/"})

    def test_rollback_is_unsolved_detectable_missing_partition_not_partial_success(self):
        state, trace = model.rollback_counterexample()
        self.assertIn("UNSOLVED", trace[-1])
        self.assertIn("missing sealed participants [0]", model.violation(state))
        with self.assertRaisesRegex(model.Unavailable, "missing sealed participants"):
            model.read_snapshot(state)


if __name__ == "__main__":
    unittest.main()
