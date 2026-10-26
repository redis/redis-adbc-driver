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


import unittest

from sqlscan import split_sql


class ScannerTests(unittest.TestCase):
    def test_semicolons_inside_every_quote_style(self):
        query = "SELECT E'it\\'s;ok', 'a;''b', \"i;d\", $tag$;--/*$tag$; SELECT 2;"
        self.assertEqual(len(split_sql(query)), 2)

    def test_nested_comments_and_psql_commands(self):
        source = (
            "\\set filename 'x'\nSELECT /* outer /* inner; */ */ 1; -- ;\nSELECT 2;"
        )
        self.assertEqual(split_sql(source), ["SELECT   1;", "SELECT 2;"])

    def test_invalid_unicode_is_a_statement_to_test(self):
        self.assertEqual(
            len(split_sql("SELECT U&'bad\\061'; SELECT E'bad\\udb99';")), 2
        )

    def test_unterminated_inputs_are_fixture_errors(self):
        for query in ("SELECT 'bad", "SELECT /* unclosed", "SELECT $x$unclosed"):
            with self.assertRaises(ValueError):
                split_sql(query)

    def test_standard_string_setting(self):
        self.assertEqual(
            len(
                split_sql(
                    "SET standard_conforming_strings=off; SELECT 'it\\'s;ok'; SELECT 2;"
                )
            ),
            3,
        )
