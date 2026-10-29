| Direction / workload | Format | Native seconds: median (range) | PyArrow seconds: median (range) | Native elapsed change |
|---|---|---:|---:|---:|
| export / aggregate-narrow | file | 0.2038 (0.1987–0.2140) | 0.2031 (0.1964–0.2157) | +0.3% |
| export / aggregate-narrow | stream | 0.2048 (0.2015–0.2068) | 0.2074 (0.2020–0.2167) | -1.3% |
| export / scan-narrow | file | 0.2581 (0.2483–0.2679) | 0.2583 (0.2540–0.2654) | -0.1% |
| export / scan-narrow | stream | 0.2733 (0.2663–0.3116) | 0.2724 (0.2625–0.2761) | +0.3% |
| export / scan-wide | file | 1.1961 (1.1882–1.2121) | 1.1871 (1.1505–1.2236) | +0.8% |
| export / scan-wide | stream | 1.1946 (1.1854–1.2206) | 1.1810 (1.1692–1.2144) | +1.2% |
| export / union-wide | file | 2.4791 (2.4559–2.5125) | 2.4885 (2.4728–2.5702) | -0.4% |
| export / union-wide | stream | 2.4467 (2.4097–2.4955) | 2.4523 (2.4332–2.4834) | -0.2% |
| import / import-narrow | file | 0.2085 (0.2028–0.2131) | 0.2052 (0.2030–0.2116) | +1.6% |
| import / import-narrow | stream | 0.2200 (0.2089–0.2321) | 0.2181 (0.2033–0.2377) | +0.9% |
| import / import-wide | file | 1.1997 (1.1910–1.2462) | 1.2116 (1.1932–1.2763) | -1.0% |
| import / import-wide | stream | 1.2438 (1.2033–1.4628) | 1.2503 (1.2078–1.3552) | -0.5% |

Negative elapsed change means a lower native median; 12/12 elapsed ranges overlap.

| Workload / format | Method | Output rows/s | IPC MiB/s | Client CPU seconds | Peak RSS MiB | Encoded MiB |
|---|---|---:|---:|---:|---:|---:|
| aggregate-narrow / file | native | 476 | 0.02 | 0.1340 | 97.8 | 0.0033 |
| aggregate-narrow / file | pyarrow | 478 | 0.02 | 0.1411 | 98.7 | 0.0033 |
| aggregate-narrow / stream | native | 474 | 0.01 | 0.1315 | 96.9 | 0.0031 |
| aggregate-narrow / stream | pyarrow | 468 | 0.01 | 0.1355 | 96.6 | 0.0031 |
| scan-narrow / file | native | 193,712 | 10.03 | 0.1307 | 98.3 | 2.5888 |
| scan-narrow / file | pyarrow | 193,555 | 10.02 | 0.1331 | 96.5 | 2.5888 |
| scan-narrow / stream | native | 182,948 | 9.47 | 0.1345 | 99.4 | 2.5882 |
| scan-narrow / stream | pyarrow | 183,547 | 9.50 | 0.1391 | 99.4 | 2.5882 |
| scan-wide / file | native | 41,803 | 33.89 | 0.7334 | 170.3 | 40.5357 |
| scan-wide / file | pyarrow | 42,121 | 34.15 | 0.7389 | 167.0 | 40.5352 |
| scan-wide / stream | native | 41,854 | 33.93 | 0.7331 | 162.0 | 40.5339 |
| scan-wide / stream | pyarrow | 42,336 | 34.32 | 0.7375 | 171.5 | 40.5334 |
| union-wide / file | native | 40,338 | 32.70 | 1.6417 | 211.8 | 81.0662 |
| union-wide / file | pyarrow | 40,184 | 32.58 | 1.6636 | 214.2 | 81.0651 |
| union-wide / stream | native | 40,871 | 33.13 | 1.6442 | 208.8 | 81.0641 |
| union-wide / stream | pyarrow | 40,778 | 33.06 | 1.6619 | 213.9 | 81.0630 |
| import-narrow / file | native | 239,807 | 12.42 | 0.0785 | 75.6 | 2.5891 |
| import-narrow / file | pyarrow | 243,641 | 12.62 | 0.0781 | 76.9 | 2.5891 |
| import-narrow / stream | native | 227,316 | 11.77 | 0.0801 | 76.5 | 2.5886 |
| import-narrow / stream | pyarrow | 229,299 | 11.87 | 0.0816 | 77.7 | 2.5886 |
| import-wide / file | native | 41,677 | 33.80 | 0.4724 | 148.9 | 40.5449 |
| import-wide / file | pyarrow | 41,267 | 33.46 | 0.4623 | 145.9 | 40.5449 |
| import-wide / stream | native | 40,200 | 32.60 | 0.4885 | 148.8 | 40.5431 |
| import-wide / stream | pyarrow | 39,992 | 32.43 | 0.4830 | 146.5 | 40.5431 |
