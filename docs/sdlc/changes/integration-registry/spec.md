# Integration registry contract

Date: 2026-11-09. Base: `cc3dad27`. Issue: [#343](https://github.com/redis/redis-adbc-driver/issues/343).
Status: implemented; final CI/delivery recorded on the PR.

One standard-library module owns candidate aliases and exact pytest selectors,
profile Python versions/extras/candidate membership, guide membership and
read/write requirements. Guide names and candidate aliases are separate public
surfaces: `huggingface-datasets`/`datasets`, `pyarrow`/`arrow`, `scikit-learn`/`ml`
and `r-dbi`/`rstudio`/`r` must remain distinct. Keep both historical execution
orders. Dependency pins stay in `python/pyproject.toml`.

Python guide runners import registry views. Bash entry points query plain lines,
check command failures and build quoted arrays without eval/source. Tool help,
profile validation, selectors and default summary profiles derive from the same
module. Reject a complete invalid request before environment provisioning or
report creation; do not ignore invalid tokens after a valid one.

Keep dbt adapter setup, Kestra preparation and R/Quarto runtime setup as explicit
hooks. Ray TLS remains a separate owned-fixture path with no plaintext pytest
selector. Preserve failure/skip accounting, stale-report removal, private-CA
isolation and TLS cleanup. Guides require successful child execution plus an
output-write/read-back marker where applicable; Fivetran and Superset retain
separate evidence contracts.

Do not merge dependency environments, add a workflow engine, remove integrations
or change Redis/SQL behavior. Offline contracts do not prove backend integration
compatibility. Real Redis tests still require verified Software effective 4x QPF.

See [intent](intent.md), [plan](plan.md), [validation](validation.md).
