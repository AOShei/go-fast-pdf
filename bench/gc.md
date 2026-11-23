# The settings of the garbage collector

Written by `go run ./bench/compare -gc`. Do not edit: run it again.

The library leaves the settings of Go's garbage collector alone: they are the program's, for the whole
process. A program that has memory to spare can set GOGC higher than 100 (the environment variable, or
debug.SetGCPercent), so that the collector runs less often, and spend less time collecting; or set a
memory limit (GOMEMLIMIT, or debug.SetMemoryLimit). This is what each setting costs and gives here.

| Setting | One worker: time | memory | 24 workers: time | memory | 24 documents at once: time | memory |
|---|---:|---:|---:|---:|---:|---:|
| `GOGC=100` (default) | 51.5 ms | 29 MB | 16.9 ms | 92 MB | 145 ms | 597 MB |
| `GOGC=200` | 48.6 ms (-6%) | 43 MB (+47%) | 13.1 ms (-22%) | 140 MB (+53%) | 121 ms (-17%) | 1,217 MB (+104%) |
| `GOGC=400` | 45.4 ms (-12%) | 55 MB (+89%) | 12.8 ms (-24%) | 234 MB (+155%) | 115 ms (-21%) | 2,109 MB (+253%) |

Time is the best run; memory is the most the process had resident, the median of 5 processes. Measured on AMD Ryzen 9 9900X 12-Core Processor (24 threads, linux/amd64) with go1.27.3-X:nodwarf5, 2026-11-23;
what each workload reads, the processor time and the number of collections are in [bench/gc.md](bench/gc.md).

## What is read

- **One document at a time, one worker**: the documents of the comparison in the README, one after another, each read with one worker (Options.Workers 1); 8 documents, 530 pages, 9.7 MB.
- **One document at a time, 24 workers**: the same documents, each read with 24 workers, one for each processor; 8 documents, 530 pages, 9.7 MB.
- **24 documents at once, one worker each**: the tuning set of the public corpus, 24 documents at once, each read with one worker: a batch, damaged files included; 1,089 documents, 4,532 pages, 191.3 MB.

Each setting is measured in processes of its own, started with GOGC and GOMEMLIMIT set and nothing else
changed; the settings take turns. A process reads every document of the workload and makes its text, as
the comparison does, and lets each document go when it is done; it does this again until its runs are
done. The time is the best run of all the processes of a setting.

## Each workload

- Processor time is that of the best run, on every processor together.
- Collections is the number of collections in the best run.
- Memory is the most that a process had resident over all its runs: the median of the processes, then the
  least and the most of them. It differs from one process to the next by which documents happen to be
  read at the same time, and by when the collector happens to run.

### One document at a time, one worker

| Setting | Time | Pages per second | Processor time | Collections | Most memory | Least and most |
|---|---:|---:|---:|---:|---:|---:|
| GOGC=100 (the default) | 51.5 ms | 10,296 | 62.8 ms | 16 | 29 MB | 29 MB to 33 MB |
| GOGC=200 | 48.6 ms | 10,898 | 53.8 ms | 5 | 43 MB | 42 MB to 44 MB |
| GOGC=400 | 45.4 ms | 11,666 | 48.4 ms | 3 | 55 MB | 54 MB to 57 MB |

### One document at a time, 24 workers

| Setting | Time | Pages per second | Processor time | Collections | Most memory | Least and most |
|---|---:|---:|---:|---:|---:|---:|
| GOGC=100 (the default) | 16.9 ms | 31,356 | 116 ms | 3 | 92 MB | 86 MB to 104 MB |
| GOGC=200 | 13.1 ms | 40,431 | 82.7 ms | 0 | 140 MB | 134 MB to 152 MB |
| GOGC=400 | 12.8 ms | 41,260 | 79.5 ms | 0 | 234 MB | 214 MB to 246 MB |

### 24 documents at once, one worker each

| Setting | Time | Pages per second | Processor time | Collections | Most memory | Least and most |
|---|---:|---:|---:|---:|---:|---:|
| GOGC=100 (the default) | 145 ms | 32,067 | 2,054 ms | 10 | 597 MB | 584 MB to 631 MB |
| GOGC=200 | 121 ms | 38,657 | 1,794 ms | 2 | 1,217 MB | 1,154 MB to 1,412 MB |
| GOGC=400 | 115 ms | 40,369 | 1,688 ms | 1 | 2,109 MB | 2,022 MB to 2,624 MB |

24 of the 1,089 documents end in an error (the corpus has damaged files on purpose); the time they take counts.

## What to make of it

- A higher GOGC lets the heap grow further between collections: GOGC=200 lets it grow to three times what
  is live, 400 to five times, where 100 lets it double. It gives the most where many goroutines make
  garbage at once: many workers, or many documents at once. With one worker, the collector already works
  on the other processors beside it, and a higher setting gives less.
- The memory is that of these documents. A larger document, or more of them at once, takes more, and the
  higher settings multiply that too; a memory limit (GOMEMLIMIT) keeps a high GOGC from going past what
  the machine has.
- A time that another job on the machine disturbed is too long, never too short; the best of the runs is
  the one disturbed least.
