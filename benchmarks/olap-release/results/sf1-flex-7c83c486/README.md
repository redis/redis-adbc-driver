# SF1 on Software Flex, main `7c83c486`

Query records and metadata from one pass of each suite on 2026-11-26, each suite on its own fresh fixture. The method, deployment and findings are in [the validation record](../../../../docs/sdlc/changes/sf1-flex-suites/validation.md).

`metadata.json`'s `driver` field (0.0.31) is the installed wheel's version. The library was built from main `7c83c486` and loaded with `REDIS_ADBC_LIBRARY`; SHA-256 `717b20d98815519e1039b4a19f5cf4d0b6e6cceca8dd5f2dda1817f2e02be036`.

Files:
- `sources.sha256`: the SF1 DuckDB sources, the same files as the earlier SF1 records.
- `<suite>/metadata.json`, `<suite>/queries.jsonl`, `<suite>/queries.sha256`: unchanged harness output and the SHA-256 of each exported query file.
- `tpch-q15-rerun/`, `tpch-q15-064eb12d/`: two focused TPC-H Q15 runs on the same fixture after the suite, with this build and with main `064eb12d`'s library (SHA-256 `d1943a44f6af7ba95545f276a25450ec41d10ee4242b7a02314e3292d052be4b`).

Median of the three timed runs (warm-up; runs). Every listed time validated against native DuckDB; failures name their phase.

## SSB

| Query | Rows | Redis median (warm-up; runs) | DuckDB median |
|---|---:|---|---:|
| Q1.1 | 1 | 1.79 s (3.98 s; 2.11 s/1.79 s/1.78 s) | 8 ms |
| Q1.2 | 1 | 491 ms (608 ms; 491 ms/493 ms/488 ms) | 5 ms |
| Q1.3 | 1 | 140 ms (157 ms; 140 ms/140 ms/140 ms) | 4 ms |
| Q2.1 | 280 | 25.16 s (33.92 s; 24.83 s/29.65 s/25.16 s) | 8 ms |
| Q2.2 | 56 | 21.24 s (21.63 s; 21.42 s/21.07 s/21.24 s) | 8 ms |
| Q2.3 | 7 | 5.07 s (5.12 s; 5.07 s/5.11 s/5.05 s) | 6 ms |
| Q3.1 | 150 | 27.17 s (28.37 s; 26.92 s/27.23 s/27.17 s) | 11 ms |
| Q3.2 | 596 | 13.68 s (13.81 s; 13.53 s/13.68 s/13.88 s) | 7 ms |
| Q3.3 | 24 | 10.14 s (10.17 s; 10.05 s/10.20 s/10.14 s) | 6 ms |
| Q3.4 | 3 | 6.75 s (7.13 s; 6.75 s/6.90 s/6.75 s) | 5 ms |
| Q4.1 | 35 | 27.04 s (29.64 s; 27.37 s/27.04 s/26.78 s) | 11 ms |
| Q4.2 | 100 | 6.51 s (7.64 s; 6.45 s/6.51 s/6.60 s) | 12 ms |
| Q4.3 | 330 | 2.06 s (2.11 s; 2.09 s/2.05 s/2.06 s) | 7 ms |

## TPC-H

| Query | Rows | Redis median (warm-up; runs) | DuckDB median |
|---|---:|---|---:|
| Q1 | 4 | timeout in warm-up | 16 ms |
| Q2 | 100 | 13.49 s (13.91 s; 13.53 s/13.49 s/13.46 s) | 4 ms |
| Q3 | 10 | timeout in warm-up | 10 ms |
| Q4 | 5 | timeout in warm-up | 14 ms |
| Q5 | 5 | 40.47 s (50.09 s; 40.47 s/40.91 s/40.19 s) | 10 ms |
| Q6 | 1 | 4.30 s (5.96 s; 4.39 s/4.30 s/4.23 s) | 4 ms |
| Q7 | 4 | 14.45 s (19.27 s; 14.45 s/14.33 s/14.77 s) | 13 ms |
| Q8 | 2 | 19.54 s (21.48 s; 19.53 s/20.30 s/19.54 s) | 11 ms |
| Q9 | 175 | timeout in warm-up | 30 ms |
| Q10 | 20 | timeout in warm-up | 24 ms |
| Q11 | 1,048 | 814 ms (1.90 s; 779 ms/828 ms/814 ms) | 5 ms |
| Q12 | 2 | 9.39 s (17.49 s; 11.28 s/9.39 s/8.88 s) | 9 ms |
| Q13 | 42 | 12.87 s (11.07 s; 13.07 s/12.87 s/12.48 s) | 48 ms |
| Q14 | 1 | 1.83 s (4.58 s; 1.93 s/1.83 s/1.81 s) | 10 ms |
| Q15 | 1 | timeout in warm-up | 6 ms |
| Q16 | 18,314 | 5.13 s (8.90 s; 5.11 s/5.22 s/5.13 s) | 15 ms |
| Q17 | 1 | 10.99 s (11.64 s; 11.00 s/10.99 s/10.96 s) | 10 ms |
| Q18 | 57 | 27.14 s (54.14 s; 26.93 s/27.14 s/27.96 s) | 32 ms |
| Q19 | 1 | 2.53 s (4.14 s; 2.54 s/2.53 s/2.41 s) | 9 ms |
| Q20 | 186 | timeout in warm-up | 12 ms |
| Q21 | 100 | timeout in warm-up | 24 ms |
| Q22 | 7 | 9.34 s (13.22 s; 9.38 s/9.06 s/9.34 s) | 12 ms |

## TPC-DS

| Query | Rows | Redis median (warm-up; runs) | DuckDB median |
|---|---:|---|---:|
| Q1 | 100 | 2.19 s (9.30 s; 2.88 s/2.19 s/1.61 s) | 15 ms |
| Q2 | 2,513 | timeout in warm-up | 14 ms |
| Q3 | 89 | 302 ms (1.53 s; 302 ms/318 ms/288 ms) | 5 ms |
| Q4 | 8 | timeout in warm-up | 62 ms |
| Q5 | 100 | 2.68 s (6.58 s; 3.01 s/2.67 s/2.68 s) | 16 ms |
| Q6 | 45 | 3.75 s (8.29 s; 4.89 s/3.49 s/3.75 s) | 10 ms |
| Q7 | 100 | 17.78 s (25.80 s; 17.78 s/16.94 s/18.44 s) | 19 ms |
| Q8 | 5 | 5.20 s (7.11 s; 5.28 s/5.20 s/5.07 s) | 11 ms |
| Q9 | 1 | 22.65 s (24.21 s; 22.65 s/22.61 s/23.39 s) | 28 ms |
| Q10 | 5 | 1.71 s (5.75 s; 1.92 s/1.71 s/1.62 s) | 11 ms |
| Q11 | 88 | 17.95 s (21.21 s; 19.97 s/17.95 s/17.06 s) | 26 ms |
| Q12 | 100 | 145 ms (326 ms; 150 ms/145 ms/141 ms) | 4 ms |
| Q13 | 1 | 5.83 s (25.02 s; 6.37 s/5.48 s/5.83 s) | 16 ms |
| Q14 | 100 | timeout in warm-up | 48 ms |
| Q15 | 100 | 3.87 s (6.12 s; 4.38 s/3.87 s/2.89 s) | 7 ms |
| Q16 | 1 | 11.31 s (32.88 s; 13.99 s/11.31 s/10.13 s) | 5 ms |
| Q17 | 1 | 1.50 s (3.77 s; 1.53 s/1.50 s/1.44 s) | 12 ms |
| Q18 | 100 | timeout in warm-up | 16 ms |
| Q19 | 100 | 6.07 s (7.47 s; 6.12 s/6.07 s/5.41 s) | 7 ms |
| Q20 | 100 | 182 ms (269 ms; 190 ms/182 ms/180 ms) | 4 ms |
| Q21 | 100 | 13.86 s (15.34 s; 12.82 s/13.86 s/13.87 s) | 3 ms |
| Q22 | 100 | timeout in warm-up | 21 ms |
| Q23 | 4 | timeout in warm-up | 78 ms |
| Q24 | 3 | 4.10 s (5.99 s; 4.10 s/3.97 s/4.11 s) | 10 ms |
| Q25 | 1 | 1.40 s (2.68 s; 1.59 s/1.40 s/1.31 s) | 7 ms |
| Q26 | 100 | 2.70 s (5.23 s; 3.01 s/2.43 s/2.70 s) | 10 ms |
| Q27 | 100 | 15.92 s (17.50 s; 15.92 s/14.24 s/16.30 s) | 20 ms |
| Q28 | 1 | timeout in warm-up | 30 ms |
| Q29 | 1 | 8.95 s (15.79 s; 9.14 s/7.80 s/8.95 s) | 11 ms |
| Q30 | 100 | 850 ms (1.95 s; 1.23 s/850 ms/833 ms) | 9 ms |
| Q31 | 51 | 15.32 s (43.63 s; 40.31 s/15.32 s/14.30 s) | 16 ms |
| Q32 | 1 | 63 ms (136 ms; 64 ms/60 ms/63 ms) | 3 ms |
| Q33 | 100 | 1.50 s (2.20 s; 1.68 s/1.50 s/1.48 s) | 8 ms |
| Q34 | 451 | 999 ms (2.57 s; 1.13 s/999 ms/870 ms) | 8 ms |
| Q35 | 100 | timeout in warm-up | 15 ms |
| Q36 | 100 | 16.07 s (18.87 s; 16.32 s/16.04 s/16.07 s) | 14 ms |
| Q37 | 1 | 327 ms (908 ms; 355 ms/327 ms/327 ms) | 5 ms |
| Q38 | 1 | 21.55 s (26.75 s; 27.69 s/21.55 s/20.20 s) | 20 ms |
| Q39 | 243 | timeout in warm-up | 12 ms |
| Q40 | 100 | timeout in warm-up | 9 ms |
| Q41 | 4 | 368 ms (441 ms; 368 ms/373 ms/329 ms) | 7 ms |
| Q42 | 10 | 394 ms (2.08 s; 482 ms/387 ms/394 ms) | 6 ms |
| Q43 | 6 | 14.34 s (15.71 s; 15.32 s/12.98 s/14.34 s) | 9 ms |
| Q44 | 10 | timeout in warm-up | 10 ms |
| Q45 | 19 | 2.15 s (6.31 s; 2.81 s/2.12 s/2.15 s) | 8 ms |
| Q46 | 100 | 2.39 s (4.61 s; 2.59 s/2.39 s/2.36 s) | 17 ms |
| Q47 | 100 | 16.88 s (16.64 s; 17.27 s/16.80 s/16.88 s) | 36 ms |
| Q48 | 1 | 21.23 s (33.44 s; 21.47 s/21.23 s/20.55 s) | 14 ms |
| Q49 | 32 | timeout in warm-up | 9 ms |
| Q50 | 6 | 8.22 s (13.30 s; 8.02 s/8.22 s/8.49 s) | 11 ms |
| Q51 | 100 | 34.69 s (58.30 s; 35.15 s/34.69 s/33.81 s) | 104 ms |
| Q52 | 100 | 400 ms (1.06 s; 452 ms/400 ms/382 ms) | 4 ms |
| Q53 | 100 | 1.52 s (2.16 s; 1.61 s/1.52 s/1.48 s) | 6 ms |
| Q54 | 1 | 1.53 s (2.26 s; 1.57 s/1.53 s/1.42 s) | 6 ms |
| Q55 | 100 | 366 ms (624 ms; 395 ms/366 ms/363 ms) | 4 ms |
| Q56 | 100 | 2.15 s (3.12 s; 2.22 s/2.14 s/2.15 s) | 7 ms |
| Q57 | 100 | 3.33 s (4.69 s; 3.72 s/3.33 s/3.20 s) | 17 ms |
| Q58 | 3 | 2.84 s (3.22 s; 2.88 s/2.83 s/2.84 s) | 7 ms |
| Q59 | 100 | 10.82 s (12.34 s; 12.23 s/10.82 s/10.16 s) | 25 ms |
| Q60 | 100 | 4.22 s (5.73 s; 4.95 s/4.22 s/4.12 s) | 8 ms |
| Q61 | 1 | 5.68 s (7.72 s; 5.68 s/5.69 s/5.36 s) | 8 ms |
| Q62 | 100 | 1.90 s (3.50 s; 2.04 s/1.88 s/1.90 s) | 5 ms |
| Q63 | 100 | 1.45 s (2.28 s; 1.44 s/1.47 s/1.45 s) | 7 ms |
| Q64 | 10 | timeout in warm-up | 27 ms |
| Q65 | 100 | 24.42 s (26.87 s; 24.42 s/23.63 s/24.85 s) | 12 ms |
| Q66 | 5 | 956 ms (3.56 s; 1.05 s/956 ms/869 ms) | 12 ms |
| Q67 | 100 | 35.56 s (36.23 s; 36.67 s/35.56 s/29.95 s) | 91 ms |
| Q68 | 100 | 1.28 s (3.73 s; 1.32 s/1.28 s/1.21 s) | 11 ms |
| Q69 | 100 | 7.87 s (43.59 s; 8.85 s/7.87 s/7.85 s) | 10 ms |
| Q70 | 3 | 28.52 s (31.14 s; 29.14 s/28.52 s/28.28 s) | 10 ms |
| Q71 | 1,018 | 1.77 s (4.02 s; 2.73 s/1.77 s/1.61 s) | 8 ms |
| Q72 | 100 | OOM-killed in warm-up | 20 ms |
| Q73 | 5 | 481 ms (2.86 s; 487 ms/481 ms/472 ms) | 6 ms |
| Q74 | 92 | 17.44 s (27.89 s; 19.21 s/17.44 s/16.02 s) | 27 ms |
| Q75 | 100 | deadline, reported as UNKNOWN | 26 ms |
| Q76 | 100 | 32.97 s (58.91 s; 34.06 s/32.97 s/32.90 s) | 11 ms |
| Q77 | 44 | 996 ms (3.27 s; 1.18 s/996 ms/972 ms) | 8 ms |
| Q78 | 100 | OOM-killed in warm-up | 67 ms |
| Q79 | 100 | 1.71 s (4.05 s; 1.76 s/1.71 s/1.61 s) | 13 ms |
| Q80 | 100 | OOM-killed in warm-up | 12 ms |
| Q81 | 100 | 1.04 s (4.05 s; 1.04 s/1.04 s/1.01 s) | 12 ms |
| Q82 | 2 | 743 ms (2.34 s; 762 ms/743 ms/739 ms) | 4 ms |
| Q83 | 21 | 2.91 s (3.32 s; 2.99 s/2.88 s/2.91 s) | 4 ms |
| Q84 | 25 | 1.47 s (2.04 s; 1.47 s/1.47 s/1.47 s) | 5 ms |
| Q85 | 6 | timeout in warm-up | 11 ms |
| Q86 | 100 | 1.31 s (2.70 s; 1.37 s/1.31 s/1.31 s) | 5 ms |
| Q87 | 1 | 21.26 s (26.89 s; 21.80 s/21.26 s/20.77 s) | 20 ms |
| Q88 | 1 | 3.07 s (5.32 s; 3.07 s/3.05 s/3.18 s) | 29 ms |
| Q89 | 100 | 1.97 s (3.40 s; 2.40 s/1.93 s/1.97 s) | 8 ms |
| Q90 | 1 | 175 ms (821 ms; 197 ms/173 ms/175 ms) | 3 ms |
| Q91 | 1 | 669 ms (1.16 s; 699 ms/669 ms/645 ms) | 5 ms |
| Q92 | 1 | 252 ms (334 ms; 252 ms/237 ms/264 ms) | 4 ms |
| Q93 | 100 | OOM-killed in warm-up | 8 ms |
| Q94 | 1 | 5.49 s (22.84 s; 5.49 s/5.49 s/5.40 s) | 6 ms |
| Q95 | 1 | OOM-killed in warm-up | 6 ms |
| Q96 | 1 | 176 ms (1.00 s; 213 ms/176 ms/176 ms) | 5 ms |
| Q97 | 1 | 16.72 s (19.42 s; 16.39 s/16.72 s/16.87 s) | 20 ms |
| Q98 | 2,516 | 285 ms (360 ms; 281 ms/289 ms/285 ms) | 9 ms |
| Q99 | 90 | 3.17 s (5.21 s; 3.17 s/3.23 s/3.15 s) | 7 ms |
