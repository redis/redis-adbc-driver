# Exercise the upstream DBI contract, including its unsupported-feature checks.
# No skip list: failures are useful compatibility evidence, not expected passes.
driver <- adbcdrivermanager::adbc_driver(Sys.getenv("REDIS_ADBC_LIBRARY"))
DBItest::make_context(
  adbi::adbi(driver),
  connect_args = list(uri = Sys.getenv("REDIS_URI")),
  name = "Redis ADBC"
)
DBItest::test_all()
