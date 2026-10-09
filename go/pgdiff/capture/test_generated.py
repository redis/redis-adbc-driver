# Copyright (c) 2026 ADBC Drivers Contributors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#         http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


import pytest
from dbt.tests.adapter.basic import files
from dbt.tests.adapter.basic.test_base import BaseSimpleMaterializations
from dbt.tests.adapter.basic.test_incremental import BaseIncremental
from dbt.tests.util import run_dbt


class TestSimpleMaterializations(BaseSimpleMaterializations):
    pass


class TestIncremental(BaseIncremental):
    pass


class TestUtils:
    @pytest.fixture(scope="class")
    def packages(self):
        return {"packages": [{"local": '{{ env_var("PGDIFF_DBT_UTILS") }}'}]}

    @pytest.fixture(scope="class")
    def seeds(self):
        return {"base.csv": files.seeds_base_csv}

    @pytest.fixture(scope="class")
    def models(self):
        return {
            "utils.sql": """{{ config(materialized='table') }}
                select id, {{ dbt_utils.generate_surrogate_key(['id', 'name']) }} as sk,
                {{ dbt_utils.safe_divide('id', 'id') }} as ratio
                from {{ ref('base') }}""",
            "unioned.sql": """{{ config(materialized='view') }}
                {{ dbt_utils.union_relations([ref('base'), ref('utils')]) }}""",
        }

    def test_generated(self, project):
        run_dbt(["deps"])
        run_dbt(["seed"])
        run_dbt(["run"])
