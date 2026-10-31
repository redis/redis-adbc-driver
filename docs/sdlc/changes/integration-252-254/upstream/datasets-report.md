# Empty typed Dataset default save cannot reload because it writes zero shards

Submitted 2026-10-30: [upstream report](https://github.com/huggingface/datasets/issues/8912).
Fix PR: [upstream pull request](https://github.com/huggingface/datasets/pull/8913).

Datasets 5.1.0 writes zero shards when saving an empty typed Dataset with default
sharding. The saved state has `_data_files: []`; load_from_disk raises
`IndexError: list index out of range` in ConcatenationTable.from_tables.
This reproduces without Redis, ADBC, network access or downloaded data:

```python
import tempfile
from pathlib import Path
import pyarrow as pa
from datasets import Dataset, load_from_disk

schema = pa.schema([("id", pa.int64()), ("label", pa.string())])
dataset = Dataset(pa.Table.from_batches([], schema=schema))
with tempfile.TemporaryDirectory() as root:
    path = Path(root) / "empty"
    dataset.save_to_disk(path)
    restored = load_from_disk(path)
    assert restored.num_rows == 0
    assert restored.features == dataset.features
    assert restored.data.schema == dataset.data.schema
```

`save_to_disk` caps automatically calculated `num_shards` at `len(self.data)`.
For zero rows this reduces a positive shard count to zero. The same expression
remains in upstream `62e0b421bb996e8536d28f4fea662a4045bab82d`.
The accompanying [patch](datasets-empty-shards.patch) retains a minimum of one
shard and includes tests for default, num_proc=2 and max_shard_size saves.

All three regression variants pass with the proposed change applied to an
isolated Datasets 5.1.0 package copy. Default nonempty save/reload and both empty
and nonempty ADBC Arrow paths also pass. Until a fixed version is released,
`save_to_disk(path, num_shards=1)` preserves an empty typed dataset's schema.

Environment: macOS arm64, Python 3.12.16, Datasets 5.1.0, PyArrow 26.0.0.
Searched upstream issues for empty save_to_disk/zero shards on October 30, 2026;
no equivalent report found in those results. This is not an exhaustive proof.
Local tracker: https://github.com/redis/redis-adbc-driver/issues/253.

Prepared with AI assistance (OpenAI Codex).
