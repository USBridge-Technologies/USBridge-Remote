import json,sys
fn,tag=sys.argv[1],sys.argv[2]; out=sys.stdin.read().strip().splitlines()
m=json.loads(out[-1]) if out and out[-1].startswith(chr(123)) else {}
f=json.load(open(fn))['frames']
off=[(x['rx_us']-x['pts_us'])/1000 for x in f];b=min(off);d=[o-b for o in off];q=max(1,len(d)//4)
print(f"{tag:26} fps {m.get('fps',0):6.1f} host {m.get('host_ms_avg',0):5.1f} lag(rx-pts,quarters) {[round(sum(d[i:i+q])/q) for i in range(0,4*q,q)]} {m.get('renderer_stats','')[:90]}")
