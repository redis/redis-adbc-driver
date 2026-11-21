#!/usr/bin/env python3
"""Finite reference model for two immutable participants; no Redis/runtime code."""

from collections import deque
from dataclasses import dataclass, replace
import json


GENERATION = "g1"
PARTICIPANTS = (0, 1)  # Registered centrally before any participant can prepare.


@dataclass(frozen=True)
class Ticket:
    generation: str
    participant: int
    prefix: str


def prefix(participant, generation=GENERATION):
    # Generation is part of the physical identity; names are never reused.
    return f"{generation}/participant-{participant}/"


@dataclass(frozen=True)
class Participant:
    phase: str = "absent"  # absent -> prepared -> sealed -> deleting -> deleted
    lease: bool = True
    receipt: bool = False  # Central knowledge of an immutable local seal.


@dataclass(frozen=True)
class State:
    decision: str = "open"  # open -> commit OR abort; never changes afterward.
    retired: bool = False  # Monotonic; rejects all future publication/new pins.
    pin: bool = False  # One representative reader; zero/nonzero retention boundary.
    known: str = "unknown"  # Caller outcome knowledge, independent of decision.
    parts: tuple = (Participant(), Participant())
    tickets: tuple = (None, None)


def change_part(state, participant, **changes):
    parts = list(state.parts)
    parts[participant] = replace(parts[participant], **changes)
    return replace(state, parts=tuple(parts))


def gc_allowed(state):
    return state.decision == "abort" or (state.retired and not state.pin)


def local_delete_fence(part, participant, ticket):
    """Local-only identity/phase check on an authenticated delivered capability."""
    expected = Ticket(GENERATION, participant, prefix(participant))
    if ticket != expected:
        return None
    if part.phase in ("deleting", "deleted"):
        return None
    return replace(part, phase="deleting")


def authorize_delete(state, participant, ticket):
    """Model message provenance, NOT a remote read inside the local fence.

    The network cannot invent an unissued capability. This ghost check models
    trusted/authenticated issuance; the actual local transition above receives
    only local state and the delivered capability, with no central-state access.
    """
    if state.tickets[participant] != ticket:
        return None
    if local_delete_fence(state.parts[participant], participant, ticket) is None:
        return None
    return change_part(state, participant, phase="deleting")


def transitions(state, unsafe_lease_gc=False):
    """All enabled events. Message omission is delay; duplicate no-ops are omitted."""
    for i, part in enumerate(state.parts):
        if part.lease:
            yield f"expire lease P{i}", change_part(state, i, lease=False)
        # Local grants were issued with the registered intent before this
        # initial state. Delivery cannot atomically inspect remote authority:
        # in-flight prepare/seal may arrive after central abort/retirement.
        # Only the local lease and irreversible local deletion fence stop them.
        if part.lease:
            if part.phase == "absent":
                yield f"prepare P{i}", change_part(state, i, phase="prepared")
            elif part.phase == "prepared":
                for lost in (False, True):
                    yield (f"seal P{i}" + ("; reply lost" if lost else ""),
                           change_part(state, i, phase="sealed", receipt=not lost))
        if part.phase == "sealed" and not part.receipt:
            yield f"read seal outcome P{i}", change_part(state, i, receipt=True)
        if gc_allowed(state) and state.tickets[i] is None:
            tickets = list(state.tickets)
            tickets[i] = Ticket(GENERATION, i, prefix(i))
            yield f"issue terminal GC ticket P{i}", replace(state, tickets=tuple(tickets))
        if state.tickets[i] is not None:
            fenced = authorize_delete(state, i, state.tickets[i])
            if fenced is not None:
                yield f"deliver GC ticket; fence P{i}", fenced
        if part.phase == "deleting":
            yield f"finish local deletion P{i}", change_part(state, i, phase="deleted")
        if unsafe_lease_gc and not part.lease and part.phase in ("prepared", "sealed"):
            yield f"UNSAFE lease-expiry deletion P{i}", change_part(state, i, phase="deleted")

    if state.decision == "open":
        # Receipts prove seals only because local retention forbids self-GC.
        # Central publication does not perform a live cross-shard row read.
        if not state.retired and all(p.receipt for p in state.parts):
            for lost in (False, True):
                yield ("central commit" + ("; reply lost" if lost else ""),
                       replace(state, decision="commit", known="unknown" if lost else "commit"))
        for lost in (False, True):
            yield ("cancel: central abort" + ("; reply lost" if lost else ""),
                   replace(state, decision="abort", known="unknown" if lost else "abort"))
    elif state.known == "unknown":
        yield "read decision after loss/cancel", replace(state, known=state.decision)
    if not state.retired:
        yield "retire generation", replace(state, retired=True)
    if state.decision == "commit" and not state.retired and not state.pin:
        yield "pin committed generation", replace(state, pin=True)
    if state.pin:
        yield "release reader pin", replace(state, pin=False)


def violation(state):
    """Safety only: no liveness or availability claim, and no partial read success."""
    if state.decision == "commit" and (not state.retired or state.pin):
        missing = [i for i, p in enumerate(state.parts) if p.phase != "sealed"]
        if missing:
            return f"visible/pinned committed generation missing sealed participants {missing}"
    if state.pin and state.decision != "commit":
        return "reader pinned an uncommitted generation"
    return None


class Unavailable(RuntimeError):
    pass


def read_snapshot(state):
    """A detectable missing participant fails the whole read, never returns a subset."""
    if state.decision != "commit" or (state.retired and not state.pin):
        raise Unavailable("no visible or pinned committed generation")
    problem = violation(state)
    if problem:
        raise Unavailable(problem)
    return PARTICIPANTS


@dataclass
class Exploration:
    states: int
    edges: int
    trace: list | None = None
    problem: str | None = None


def explore(unsafe_lease_gc=False, max_states=100000):
    """Exhaust reachable finite states, or raise rather than claim a truncated pass."""
    initial = State()
    parents = {initial: None}
    queue = deque([initial])
    edges = 0
    while queue:
        state = queue.popleft()
        problem = violation(state)
        if problem:
            trace = []
            cursor = state
            while parents[cursor] is not None:
                previous, action = parents[cursor]
                trace.append(action)
                cursor = previous
            return Exploration(len(parents), edges, list(reversed(trace)), problem)
        for action, successor in transitions(state, unsafe_lease_gc):
            edges += 1
            if successor not in parents:
                if len(parents) >= max_states:
                    raise RuntimeError("state bound reached: exploration INCOMPLETE")
                parents[successor] = (state, action)
                queue.append(successor)
    return Exploration(len(parents), edges)


@dataclass(frozen=True)
class LocalObject:
    generation: str
    participant: int
    prefix: str
    present: bool = True


def allocate(used_prefixes, generation, participant, physical_prefix):
    """Sequential toy registry, not a concurrent/distributed allocation algorithm."""
    if physical_prefix in used_prefixes:
        raise ValueError("physical prefix must never be reused")
    used_prefixes.add(physical_prefix)
    return LocalObject(generation, participant, physical_prefix)


def apply_ticket(obj, ticket, generation_check=True):
    matches = obj.prefix == ticket.prefix and obj.participant == ticket.participant
    if generation_check:
        matches = matches and obj.generation == ticket.generation
    return replace(obj, present=False) if matches else obj


def prefix_reuse_counterexample():
    """Deliberately bypass allocation policy to expose delayed prefix-only GC."""
    old = Ticket("old-generation", 0, "reused-prefix/")
    replacement = LocalObject("new-pinned-generation", 0, "reused-prefix/")
    damaged = apply_ticket(replacement, old, generation_check=False)
    assert not damaged.present
    return ["abort old generation; issue GC ticket", "delay old ticket",
            "UNSAFE reuse prefix for new committed/pinned generation",
            "deliver prefix-only old ticket; new participant deleted"]


def rollback_counterexample():
    """Central durability rollback is outside the positive explorer's assumptions."""
    sealed = Participant(phase="sealed", receipt=True)
    committed = State(decision="commit", known="commit", parts=(sealed, sealed))
    retired = replace(committed, retired=True)
    ticket = Ticket(GENERATION, 0, prefix(0))
    authorized = replace(retired, tickets=(ticket, None))
    fenced = authorize_delete(authorized, 0, ticket)
    deleted = change_part(fenced, 0, phase="deleted")
    # Only central durable history rolls back; the remote deletion survives.
    rolled_back = replace(committed, parts=deleted.parts)
    return rolled_back, ["commit both sealed participants", "retire; zero pins",
                         "issue terminal ticket; delete P0",
                         "UNSOLVED: central restore loses retirement but remote deletion survives"]


def main():
    safe = explore()
    unsafe = explore(unsafe_lease_gc=True)
    rollback, rollback_trace = rollback_counterexample()
    print(json.dumps({
        "scope": "finite reference model; not a Redis implementation proof",
        "repaired_no_rollback": vars(safe),
        "unsafe_lease_gc": vars(unsafe),
        "unsafe_prefix_reuse": prefix_reuse_counterexample(),
        "durability_rollback": {"status": "UNSOLVED", "trace": rollback_trace,
                                "detectable_failure": violation(rollback)},
    }, indent=2))
    return 0 if safe.problem is None and unsafe.problem is not None else 1


if __name__ == "__main__":
    raise SystemExit(main())
