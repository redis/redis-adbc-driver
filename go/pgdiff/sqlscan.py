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


"""Split regression scripts without interpreting intentionally invalid literals.

Handles PostgreSQL quotes, E strings, dollar strings, nested comments and psql
line commands. Unlike a validating lexer, invalid Unicode escapes remain test SQL.
"""

import re


def split_sql(text):
    result, buf = [], []
    i, standard_strings = 0, True
    while i < len(text):
        c = text[i]
        if c == "\\" and (i == 0 or text[i - 1] == "\n"):
            end = text.find("\n", i)
            i = len(text) if end < 0 else end + 1
            continue
        if text.startswith("--", i):
            end = text.find("\n", i)
            i = len(text) if end < 0 else end
            buf.append(" ")
            continue
        if text.startswith("/*", i):
            depth = 1
            i += 2
            while i < len(text) and depth:
                if text.startswith("/*", i):
                    depth += 1
                    i += 2
                elif text.startswith("*/", i):
                    depth -= 1
                    i += 2
                else:
                    i += 1
            if depth:
                raise ValueError("unterminated SQL comment")
            buf.append(" ")
            continue
        if c in ("'", '"'):
            quote, start = c, i
            escaped = c == "'" and (
                not standard_strings
                or (
                    i > 0
                    and text[i - 1] in "Ee"
                    and (i < 2 or not (text[i - 2].isalnum() or text[i - 2] == "_"))
                )
            )
            i += 1
            while i < len(text):
                if escaped and text[i] == "\\":
                    i += 2
                    continue
                if text[i] == quote:
                    if i + 1 < len(text) and text[i + 1] == quote:
                        i += 2
                        continue
                    i += 1
                    break
                i += 1
            else:
                raise ValueError("unterminated SQL quoted string/identifier")
            buf.append(text[start:i])
            continue
        if c == "$":
            match = re.match(r"\$(?:[A-Za-z_][A-Za-z_0-9]*)?\$", text[i:])
            if match:
                tag = match[0]
                end = text.find(tag, i + len(tag))
                if end < 0:
                    raise ValueError("unterminated dollar string")
                end += len(tag)
                buf.append(text[i:end])
                i = end
                continue
        if c == ";":
            query = "".join(buf).strip()
            if query:
                result.append(query + ";")
                setting = re.match(
                    r"SET\s+standard_conforming_strings\s*(?:TO|=)\s*(on|off)",
                    query,
                    re.IGNORECASE,
                )
                if setting:
                    standard_strings = setting[1].lower() == "on"
            buf = []
            i += 1
            continue
        buf.append(c)
        i += 1
    query = "".join(buf).strip()
    if query:
        result.append(query)
    return result
