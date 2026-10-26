"""Prepare an isolated #411 experimental source copy; never edits the checkout.

Run from the repository root. Build the printed directory using the normal
c-shared release command. The variant is a benchmark instrument, not a supported
driver option. All transformations assert the pinned source shape.
"""
import argparse
import io
from pathlib import Path
import subprocess
import tarfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--source', default='4b0fdb5ce2a1a29651bad778aa58e4198ead0e70')
parser.add_argument('--out', type=Path, required=True)
parser.add_argument('--adopt', action='store_true', help='Adopt fetched maps instead of merging fields')
parser.add_argument('--page', type=int, default=1869)
parser.add_argument('--pipeline', type=int, default=1000)
args = parser.parse_args()
if not 1 <= args.page <= 10000 or not 1 <= args.pipeline <= 10000:
    parser.error('page and pipeline must be bounded by 1..10000')
args.out.mkdir(parents=True, exist_ok=False)
archive = subprocess.check_output(['git', 'archive', args.source])
with tarfile.open(fileobj=io.BytesIO(archive)) as source:
    source.extractall(args.out, filter='data')


def replace(path, old, new):
    text = path.read_text()
    if text.count(old) != 1:
        raise ValueError(f'Expected exactly one match in {path}: {old[:80]}')
    path.write_text(text.replace(old, new))


query = args.out / 'go/query.go'
replace(query, '\tif req.ties != nil {\n\t\tit.ties =', '''
	// #411 isolated experiment only. Keep generic/narrow/ordered paths intact.
	if managedRowIDTable(meta) && len(it.need)*2 > len(meta.Columns) &&
		req.where.residual == nil && req.ties == nil && req.late == nil &&
		(len(req.sortBy) == 0 || len(req.sortBy) == 1 && req.sortBy[0].field == rowIDField && !req.sortBy[0].desc) && req.limit == nil {
		_, allFields, _ := e.newScanIter(ctx, req, params)
		it.store, it.fetch, it.experimentHMGET = e.store, allFields, true
		ar.load, ar.loadAll = []string{"__key", rowIDField}, false
		ar.experimentPage = ''' + str(args.page) + '''
	}
	if req.ties != nil {
		it.ties =''')
replace(query, '\tfetch []string\n', '\tfetch []string\n\texperimentHMGET bool // #411 isolated experiment\n')
aggregate = args.out / 'go/aggregate.go'
replace(aggregate, '\twidth     int  //', '\texperimentPage int // #411 isolated experiment\n\twidth     int  //')
replace(aggregate, 'func (r *aggRequest) pageRows() int {\n',
        'func (r *aggRequest) pageRows() int {\n\tif r.experimentPage > 0 { return r.experimentPage }\n')
# Restrict the pipeline variation to fetchRows, leaving ingestion and other
# production uses of pipelineChunk unchanged.
store = args.out / 'go/store.go'
text = store.read_text()
start = text.index('func (s *store) fetchRows(')
end = text.index('\nfunc ', start + 1)
body = text[start:end].replace('pipelineChunk', str(args.pipeline))
store.write_text(text[:start] + body + text[end:])
if args.adopt:
    # Only adopt on the explicitly marked experimental discovery path.
    # Other existing narrow fetch paths continue merging exactly as before.
    replace(query, '\t\t\tfor f, v := range raw {\n\t\t\t\tpage[i][f] = v\n\t\t\t}', '''
			if it.experimentHMGET {
				for f, v := range page[i] { if f != rowIDField { raw[f] = v } }
				page[i] = raw
			} else {
				for f, v := range raw { page[i][f] = v }
			}''')
subprocess.run(['gofmt', '-w', str(query), str(aggregate), str(store)], check=True)
print(args.out / 'go')
