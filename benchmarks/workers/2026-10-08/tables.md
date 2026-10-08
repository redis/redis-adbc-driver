| Query | Rows out | 1 worker (ms) | 2 workers (ms) | 4 workers (ms) | 8 workers (ms) | Auto (10) (ms) | Auto vs 1 |
|---|---:|---:|---:|---:|---:|---:|---:|
| Ungrouped `AVG(discount)` | 1 | 656.7 | 431.9 | 357.9 | 309.3 | 298.5 | 2.20× |
| Ungrouped `SUM, AVG, MIN, MAX` | 1 | 729.9 | 479.5 | 387.1 | 360.4 | 330.3 | 2.21× |
| Filtered ungrouped `SUM, AVG, MIN, MAX` | 1 | 239.6 | 159.2 | 133.2 | 132.4 | 116.2 | 2.06× |
| Indexed range `COUNT` (control) | 1 | 6.4 | 6.3 | 6.4 | 6.7 | 6.0 | — |
| Indexed `GROUP BY, COUNT, SUM` (control) | 6 | 9.1 | 9.4 | 8.8 | 10.0 | 9.4 | — |
| Driver grouped `AVG(discount)` (control) | 6 | 695.0 | 704.3 | 682.9 | 732.2 | 706.0 | — |
| Full scan, all columns (control) | 100,000 | 1049.8 | 1042.5 | 1052.3 | 1020.5 | 1044.0 | — |

| Query | Setting | Actual grant | CPU ms/query | Mean client CPU cores | Median process peak RSS (MiB) | Max process peak RSS (MiB) | Block-median latency range (ms) |
|---|---:|---:|---:|---:|---:|---:|---:|
| Ungrouped `AVG(discount)` | 1 | 1 | 435.2 | 0.66 | 125.6 | 127.8 | 646.6–680.8 |
| Ungrouped `AVG(discount)` | 2 | 2 | 445.4 | 1.01 | 149.4 | 152.8 | 408.3–481.8 |
| Ungrouped `AVG(discount)` | 4 | 4 | 480.4 | 1.28 | 195.9 | 211.2 | 349.6–425.3 |
| Ungrouped `AVG(discount)` | 8 | 8 | 502.3 | 1.66 | 251.2 | 273.0 | 286.3–333.5 |
| Ungrouped `AVG(discount)` | auto | 10 | 518.5 | 1.71 | 272.1 | 287.1 | 271.0–311.6 |
| Ungrouped `SUM, AVG, MIN, MAX` | 1 | 1 | 532.9 | 0.72 | 128.6 | 130.3 | 728.9–736.0 |
| Ungrouped `SUM, AVG, MIN, MAX` | 2 | 2 | 531.9 | 1.01 | 151.8 | 160.3 | 463.6–613.2 |
| Ungrouped `SUM, AVG, MIN, MAX` | 4 | 4 | 583.2 | 1.44 | 188.6 | 200.7 | 365.4–421.6 |
| Ungrouped `SUM, AVG, MIN, MAX` | 8 | 8 | 627.0 | 1.75 | 284.8 | 315.6 | 301.5–369.2 |
| Ungrouped `SUM, AVG, MIN, MAX` | auto | 10 | 639.2 | 1.89 | 297.0 | 309.9 | 309.4–365.6 |
| Filtered ungrouped `SUM, AVG, MIN, MAX` | 1 | 1 | 163.4 | 0.68 | 128.6 | 130.5 | 216.1–246.6 |
| Filtered ungrouped `SUM, AVG, MIN, MAX` | 2 | 2 | 160.1 | 0.93 | 150.0 | 151.0 | 149.8–249.5 |
| Filtered ungrouped `SUM, AVG, MIN, MAX` | 4 | 4 | 179.3 | 1.31 | 164.0 | 168.9 | 130.5–166.6 |
| Filtered ungrouped `SUM, AVG, MIN, MAX` | 8 | 8 | 191.8 | 1.43 | 163.1 | 169.1 | 114.3–169.7 |
| Filtered ungrouped `SUM, AVG, MIN, MAX` | auto | 10 | 194.8 | 1.45 | 160.4 | 163.1 | 111.3–208.0 |
| Indexed range `COUNT` (control) | 1 | 0 | 1.7 | 0.26 | 93.3 | 93.9 | 6.1–7.6 |
| Indexed range `COUNT` (control) | 2 | 0 | 1.7 | 0.27 | 93.6 | 93.9 | 5.9–6.6 |
| Indexed range `COUNT` (control) | 4 | 0 | 1.7 | 0.21 | 93.4 | 94.1 | 5.6–7.2 |
| Indexed range `COUNT` (control) | 8 | 0 | 1.7 | 0.23 | 93.6 | 93.7 | 5.8–10.3 |
| Indexed range `COUNT` (control) | auto | 0 | 1.7 | 0.23 | 93.7 | 94.0 | 5.7–8.2 |
| Indexed `GROUP BY, COUNT, SUM` (control) | 1 | 0 | 1.7 | 0.19 | 93.6 | 94.0 | 8.2–11.0 |
| Indexed `GROUP BY, COUNT, SUM` (control) | 2 | 0 | 1.8 | 0.18 | 93.7 | 93.8 | 9.0–10.0 |
| Indexed `GROUP BY, COUNT, SUM` (control) | 4 | 0 | 1.8 | 0.16 | 93.5 | 93.8 | 8.7–21.4 |
| Indexed `GROUP BY, COUNT, SUM` (control) | 8 | 0 | 1.7 | 0.14 | 93.8 | 94.2 | 8.5–24.0 |
| Indexed `GROUP BY, COUNT, SUM` (control) | auto | 0 | 1.8 | 0.19 | 93.7 | 93.9 | 9.1–9.8 |
| Driver grouped `AVG(discount)` (control) | 1 | 0 | 477.7 | 0.69 | 126.3 | 128.6 | 689.8–725.2 |
| Driver grouped `AVG(discount)` (control) | 2 | 0 | 472.1 | 0.65 | 127.6 | 128.4 | 673.7–761.0 |
| Driver grouped `AVG(discount)` (control) | 4 | 0 | 472.9 | 0.68 | 127.2 | 128.5 | 645.7–746.4 |
| Driver grouped `AVG(discount)` (control) | 8 | 0 | 481.7 | 0.65 | 126.3 | 127.0 | 672.9–751.6 |
| Driver grouped `AVG(discount)` (control) | auto | 0 | 475.2 | 0.66 | 125.8 | 127.7 | 636.3–811.6 |
| Full scan, all columns (control) | 1 | 0 | 933.3 | 0.87 | 289.6 | 317.0 | 1030.9–1166.5 |
| Full scan, all columns (control) | 2 | 0 | 931.6 | 0.87 | 304.1 | 329.6 | 981.2–1146.2 |
| Full scan, all columns (control) | 4 | 0 | 941.0 | 0.85 | 304.7 | 310.8 | 1033.8–1259.6 |
| Full scan, all columns (control) | 8 | 0 | 933.1 | 0.89 | 294.4 | 309.4 | 1006.4–1084.1 |
| Full scan, all columns (control) | auto | 0 | 941.3 | 0.87 | 301.7 | 315.1 | 1016.2–1094.4 |
