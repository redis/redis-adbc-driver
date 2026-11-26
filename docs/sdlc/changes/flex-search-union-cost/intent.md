# Intent

Requested 2026-11-25 by the user: "can you work on 648, 658, 636, 663 and 665", then "change those github issues to focus on redis fex" and "make sure to use the newest code for the driver". Base: main `16032de1`, then `064eb12d`. See [spec](spec.md), [plan](plan.md), [validation](validation.md).

On Redis Software Flex, every term of a Search key union costs milliseconds per evaluation, about 2–137 ms depending on the column (RAM: about 45 µs, #677). A read-only probe and a full SF1 Flex pass of TPC-H on newest main found two places where the driver pays that cost more often than it has to.

1. **Repeated evaluations ([#665](https://github.com/redis/redis-adbc-driver/issues/665), Q7).**
   - Q7's lineitem reader uses #644's shared complete candidates. Its filter is the ship-date window AND the 798-supplier key union, about 8 s per evaluation.
   - The reader evaluated that filter three times: `flexReaderPlan` counted it, `sharedFlexCandidates` counted it again, then it fetched.
   - So about 24 s of Q7's best 31 s run was Search work that one evaluation could do.
2. **Correlated key demands ([#648](https://github.com/redis/redis-adbc-driver/issues/648)'s pass, Q2).**
   - Q2 validated at a 20 s median on main `fdd49f7c`, and took 13.9 s there in this window. On `16032de1` it took 97 s, and timed out at 60 s in the suite.
   - #644 let a Flex streamed join push a fact key demand of up to 4,096 keys. Q2's correlated minimum-cost subquery has a 1,987-supplier demand, and runs once per part (747 parts).
   - Each run's Search calls cost about 32 ms, where its own part key selects four rows.

Scope:
- Flex only. RAM databases have no plain Flex index, and their demand limit is unchanged.
- Fewer Search evaluations, with unchanged candidate sets, budgets, visibility proofs, residual predicates and fallbacks.
- Uncorrelated Flex streamed joins keep #644's 4,096-key demand.
- No new key-demand limit. The probe showed a wider one would cost far more than it saves; see [validation](validation.md#rejected-a-wider-flex-key-demand).
