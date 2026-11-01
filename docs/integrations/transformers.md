# Redis integration guide for Hugging Face Transformers

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run


```sh
python -m pip install torch transformers tokenizers
```

Save the following as `example.py` in your working directory and run
`python example.py`. The shared helper creates a unique Redis table with three
rows, including NULLs and Unicode, and removes it when the example finishes.

```python
from integration_guide_data import example_table, read_rows
import os
os.environ['HF_HUB_OFFLINE'] = '1'
import torch
from tokenizers import Tokenizer
from tokenizers.models import WordLevel
from tokenizers.pre_tokenizers import Whitespace
from transformers import BertConfig, BertModel, PreTrainedTokenizerFast

with example_table() as name:
    table = read_rows(name)
local = Tokenizer(WordLevel({'[UNK]': 0, '[PAD]': 1, 'alpha': 2, '雪': 3}, unk_token='[UNK]'))
local.pre_tokenizer = Whitespace()
tokenizer = PreTrainedTokenizerFast(tokenizer_object=local, unk_token='[UNK]', pad_token='[PAD]')
batch = tokenizer([x or '' for x in table['label'].to_pylist()], padding=True, return_tensors='pt')
assert batch['attention_mask'].tolist() == [[1], [0], [1]]
model = BertModel(BertConfig(vocab_size=4, hidden_size=16, num_hidden_layers=1,
                            num_attention_heads=2, intermediate_size=32)).eval()
with torch.no_grad():
    output = model(**batch).last_hidden_state
assert output.shape == (3, 1, 16) and torch.isfinite(output).all()
model.save_pretrained('redis-local-model')
restored = BertModel.from_pretrained('redis-local-model', local_files_only=True).eval()
with torch.no_grad():
    torch.testing.assert_close(restored(**batch).last_hidden_state, output)
print(tuple(output.shape))

# Persist the tool's computed output, then read it through a fresh connection.
from integration_guide_data import write_result
import pyarrow as pa
write_result(pa.table({'embedding_sum': output.sum(dim=(1, 2)).detach().numpy().astype('float64')}))
```

## Expected result

The model prints `(3, 1, 16)` and offline save/reload preserves its output.

## Limits and validation

The randomly initialized tiny model demonstrates data preparation only. It downloads no pretrained model, does not train, and makes no inference-quality claim. NULL labels become empty strings.

The example follows the [saved workflow](../../python/tests/test_model_candidates.py). Historical execution is
recorded in the [native sweep](../sdlc/changes/candidate-test-runner/native-sweep.md)
or [follow-up](../sdlc/changes/native-candidate-followup/validation.md).
The [guide validation record](../sdlc/changes/integration-guides-262/validation.md)
reports the checks of these actual instructions.

From the repository root, with this tool installed, rerun the actual guide block:

```sh
python python/scripts/check_integration_guide.py transformers
```

The broader workflow can also be checked with
`python/scripts/run-candidate-tests.sh transformers` after installing its test extra.


## Write results back to Redis

The runnable example writes the tool's computed output through the shared
`write_result` Arrow sink and checks exact rows and types on a fresh connection.
This is custom application code and fully materializes the output; it does not
add a native connector, distributed sink, model/artifact store, or transactions.
The helper creates a unique output table and drops it after verification. In a
production workflow, choose an owned destination and explicit retry policy;
failed autocommit writes can partially apply and blind append retries duplicate rows.

[All integration guides](README.md)
