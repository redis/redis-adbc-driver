import contextlib
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

import compare


class Comparisons(unittest.TestCase):
    def test_unresolved_trials_are_excluded_from_ratios(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            placement = {k:dict(provider='aws',region='test',az='zone',evidence='fixture') for k in ('runner','database')}
            run = {'metadata':dict(loaded_rows=99997497,official_scale=True,placement=placement),
                   'result':[[2,2,2]] * 43}
            baseline = dict(system='baseline',machine='fixture',result=[[1,1,1]] * 43)
            (root/'run.json').write_text(json.dumps(run))
            (root/'baseline.json').write_text(json.dumps(baseline))
            for status, ratio in [('needs_tie_validation',False),('valid',True)]:
                (root/'run-correctness.json').write_text(json.dumps({'checks':[
                    dict(query=1,trial=t,status=status) for t in (1,2,3)]}))
                output = io.StringIO()
                with patch.object(sys,'argv',['compare.py',str(root/'run.json'),str(root/'baseline.json')]), contextlib.redirect_stdout(output):
                    compare.main()
                row = next(line for line in output.getvalue().splitlines() if line.startswith('| 1 |'))
                self.assertEqual('×' in row, ratio)
                self.assertEqual('(unverified)' in row, not ratio)


if __name__ == '__main__':
    unittest.main()
