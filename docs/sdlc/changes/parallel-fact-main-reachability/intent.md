# Intent

Date: 2026-11-24. Status: implementation and unit verification pending.

PR612 merged into PR607's retained branch after PR607 had already merged into main. GitHub's merged state therefore did not make the parallel fact reader reachable from main. Reapply only PR612's public delta to current main, retaining its current candidate cap and existing strict HASH error guard. Include the separately reproduced visitor-panic worker-join fix. No private source ancestry, leaf-allocation or alias-prefilter production is included.
