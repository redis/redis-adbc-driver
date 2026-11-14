"""Use unchanged full-value read workers; timed mode list excludes raw control."""
import os,sys
sys.path.insert(0,os.environ['PERF_READ_HARNESS'])
import arrow_compare
arrow_compare.base.MODES=('managed','unmanaged')
class Benchmark(arrow_compare.Benchmark):
 def run_reads(self):
  try:super().run_reads()
  finally:
   with arrow_compare.base.client(self.cfg) as c:
    assert c.get(arrow_compare.base.MARKER)==self.owner.encode(),'Foreign owner; refuse cleanup'
    for index in c.execute_command('FT._LIST'):c.execute_command('FT.DROPINDEX',index)
    c.flushdb()
    assert c.dbsize()==0 and not c.execute_command('FT._LIST')
arrow_compare.Benchmark=Benchmark
if __name__=='__main__': arrow_compare.main()
