import pytest


def query_arrow(uri, sql):
    from adbc_driver_redis import dbapi

    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.execute(sql)
        return cur.fetch_arrow_table()


def test_numpy_from_redis_adbc_arrow_result(uri):
    np = pytest.importorskip("numpy")
    table = query_arrow(
        uri,
        "SELECT 1 AS value UNION ALL SELECT 2 UNION ALL SELECT 3 ORDER BY value",
    )

    values = table.column("value").to_numpy()
    assert np.array_equal(values, np.array([1, 2, 3]))


def test_scikit_learn_model_from_redis_adbc_arrow_result(uri):
    np = pytest.importorskip("numpy")
    pytest.importorskip("sklearn")
    from sklearn.tree import DecisionTreeClassifier

    table = query_arrow(
        uri,
        "SELECT 0 AS x, 0 AS y, 0 AS label UNION ALL "
        "SELECT 0, 1, 1 UNION ALL SELECT 1, 0, 1 UNION ALL SELECT 1, 1, 0",
    )
    features = np.column_stack(
        [table.column("x").to_numpy(), table.column("y").to_numpy()]
    )
    labels = table.column("label").to_numpy()

    model = DecisionTreeClassifier(max_depth=2, random_state=0).fit(features, labels)
    assert model.predict(features).tolist() == [0, 1, 1, 0]
