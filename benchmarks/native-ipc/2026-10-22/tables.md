| Direction / workload | Format | Native seconds: median (range) | PyArrow seconds: median (range) | Native elapsed change |
|---|---|---:|---:|---:|
| export / aggregate-narrow | file | 0.2080 (0.2010–0.2110) | 0.2118 (0.2031–0.2159) | -1.8% |
| export / aggregate-narrow | stream | 0.2119 (0.2113–0.2175) | 0.2069 (0.1992–0.2174) | +2.4% |
| export / scan-narrow | file | 0.2517 (0.2498–0.2689) | 0.2603 (0.2530–0.2650) | -3.3% |
| export / scan-narrow | stream | 0.2520 (0.2446–0.2585) | 0.2579 (0.2525–0.2684) | -2.3% |
| export / scan-wide | file | 1.1894 (1.1552–1.2342) | 1.2051 (1.1712–1.2231) | -1.3% |
| export / scan-wide | stream | 1.2045 (1.1965–1.2535) | 1.1935 (1.1792–1.2215) | +0.9% |
| export / union-wide | file | 2.4750 (2.4446–2.4792) | 2.4746 (2.4378–2.5534) | +0.0% |
| export / union-wide | stream | 2.4736 (2.4224–2.4998) | 2.4772 (2.4385–2.4882) | -0.1% |
| import / import-narrow | file | 0.2036 (0.1928–0.2115) | 0.1977 (0.1936–0.2239) | +2.9% |
| import / import-narrow | stream | 0.1939 (0.1917–0.1994) | 0.1966 (0.1922–0.1975) | -1.4% |
| import / import-wide | file | 1.1694 (1.1545–1.4239) | 1.1860 (1.1608–1.3484) | -1.4% |
| import / import-wide | stream | 1.2365 (1.1814–1.2458) | 1.1915 (1.1665–1.3762) | +3.8% |

Negative elapsed change means a lower native median; 12/12 elapsed ranges overlap.

| Workload / format | Method | Output rows/s | IPC MiB/s | Client CPU seconds | Peak RSS MiB | Encoded MiB |
|---|---|---:|---:|---:|---:|---:|
| aggregate-narrow / file | native | 466 | 0.02 | 0.1346 | 104.5 | 0.0033 |
| aggregate-narrow / file | pyarrow | 458 | 0.02 | 0.1382 | 105.5 | 0.0033 |
| aggregate-narrow / stream | native | 458 | 0.01 | 0.1362 | 102.1 | 0.0031 |
| aggregate-narrow / stream | pyarrow | 469 | 0.01 | 0.1373 | 104.5 | 0.0031 |
| scan-narrow / file | native | 198,646 | 10.28 | 0.1326 | 104.5 | 2.5888 |
| scan-narrow / file | pyarrow | 192,095 | 9.95 | 0.1347 | 104.2 | 2.5888 |
| scan-narrow / stream | native | 198,417 | 10.27 | 0.1309 | 102.5 | 2.5882 |
| scan-narrow / stream | pyarrow | 193,875 | 10.04 | 0.1371 | 105.0 | 2.5882 |
| scan-wide / file | native | 42,039 | 34.08 | 0.7285 | 174.7 | 40.5357 |
| scan-wide / file | pyarrow | 41,489 | 33.64 | 0.7416 | 182.9 | 40.5352 |
| scan-wide / stream | native | 41,510 | 33.65 | 0.7253 | 178.8 | 40.5339 |
| scan-wide / stream | pyarrow | 41,895 | 33.96 | 0.7379 | 181.5 | 40.5334 |
| union-wide / file | native | 40,404 | 32.75 | 1.6598 | 225.9 | 81.0662 |
| union-wide / file | pyarrow | 40,410 | 32.76 | 1.6548 | 238.3 | 81.0651 |
| union-wide / stream | native | 40,427 | 32.77 | 1.6517 | 214.7 | 81.0641 |
| union-wide / stream | pyarrow | 40,368 | 32.72 | 1.6689 | 222.9 | 81.0630 |
| import-narrow / file | native | 245,604 | 12.72 | 0.0771 | 82.6 | 2.5891 |
| import-narrow / file | pyarrow | 252,848 | 13.09 | 0.0765 | 84.6 | 2.5891 |
| import-narrow / stream | native | 257,906 | 13.35 | 0.0748 | 82.3 | 2.5886 |
| import-narrow / stream | pyarrow | 254,375 | 13.17 | 0.0767 | 83.7 | 2.5886 |
| import-wide / file | native | 42,757 | 34.67 | 0.4529 | 155.7 | 40.5449 |
| import-wide / file | pyarrow | 42,157 | 34.19 | 0.4486 | 153.1 | 40.5449 |
| import-wide / stream | native | 40,438 | 32.79 | 0.4618 | 155.7 | 40.5431 |
| import-wide / stream | pyarrow | 41,962 | 34.03 | 0.4475 | 153.5 | 40.5431 |
