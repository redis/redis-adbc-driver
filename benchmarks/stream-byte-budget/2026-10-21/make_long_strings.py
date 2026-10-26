from pathlib import Path
import hashlib,json
import pyarrow as pa
import pyarrow.parquet as pq
src=Path('/private/tmp/hits-prefix-374.parquet');out=Path('/private/tmp/redis-remaining/hits-varied-strings.parquet')
t=pq.read_table(src);i=t.schema.get_field_index('SearchPhrase')
values=[(v or '')+'\x00'+('Ωx'*[0,16,256,2048][j%4]) for j,v in enumerate(t.column(i).to_pylist())]
t=t.set_column(i,t.schema.field(i),pa.array(values,type=t.schema.field(i).type));pq.write_table(t,out,compression='zstd')
s={'source_sha256':hashlib.sha256(src.read_bytes()).hexdigest(),'derived_sha256':hashlib.sha256(out.read_bytes()).hexdigest(),'transform':'SearchPhrase := original + NUL + Omega-x repeated [0,16,256,2048][row_index % 4]','rows':t.num_rows,'columns':t.num_columns,'min_utf8_bytes':min(map(lambda v:len(v.encode()),values)),'max_utf8_bytes':max(map(lambda v:len(v.encode()),values)),'mean_utf8_bytes':sum(len(v.encode()) for v in values)/len(values),'scope':'derived width/string sensitivity control, not unmodified ClickBench'}
Path('/private/tmp/redis-remaining/varied-strings.json').write_text(json.dumps(s,indent=2)+'\n')
print(json.dumps(s))
