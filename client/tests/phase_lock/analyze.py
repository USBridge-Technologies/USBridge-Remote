# usage: analyze.py run.csv offset_us [skip_s]
import csv,sys
from statistics import median
import bisect
fn=sys.argv[1];clock=[tuple(map(float,l.split())) for l in open(sys.argv[2]) if len(l.split())==2];skip=float(sys.argv[3]) if len(sys.argv)>3 else 4
clock.sort();ct=[c[0] for c in clock]
def offs(wall_us):
    # Mac wall - this wall at this wall time, interpolated between samples
    t=wall_us/1e6;i=bisect.bisect(ct,t)
    if i<=0:return clock[0][1]
    if i>=len(clock):return clock[-1][1]
    a,b=clock[i-1],clock[i];return a[1]+(b[1]-a[1])*(t-a[0])/(b[0]-a[0])
lines=open(fn).read().splitlines();wmm=int(lines[0].split('wall_minus_mono_us=')[1].split()[0])
R=[dict(zip(lines[1].split(','),map(int,l.split(',')))) for l in lines[2:]]
# segment by arm changes, dropping the first seconds of each segment
segs=[];
for r in R:
    if not segs or segs[-1][0]!=r['arm']: segs.append((r['arm'],[]))
    segs[-1][1].append(r)
arms={}
for arm,rs in segs:
    t0=next((x['ready_us'] for x in rs),0)
    arms.setdefault(arm,[]).extend(x for x in rs if x['ready_us']>=t0+skip*1e6)
def pct(a,q): a=sorted(a); return a[min(len(a)-1,int(len(a)*q))] if a else float('nan')
print(f"{'arm':6}{'frames':>7}{'render->glass p50':>19}{'p95':>7}{'p99':>7}{'ready->glass p50':>18}{'p95':>7}{'late%':>7}{'repeat%':>9}{'dropped%':>9}{'barOK%':>8}")
out={}
for arm in sorted(arms):
    rs=arms[arm]; P=[r for r in rs if r['status']==1]
    e=[]
    for r in P:
        if r['bar_ok']:
            w=r['present_us']+wmm;d=((w+offs(w))%(1<<32)-r['bar'])%(1<<32)
            if d<5e6: e.append(d/1000)
    w=[(r['present_us']-r['ready_us'])/1000 for r in P]
    T=median([r['refresh_ns']/1e6 for r in P]) if P else 16.67
    gaps=[(b['present_us']-a['present_us'])/1000 for a,b in zip(P,P[1:])]
    rep=sum(1 for g in gaps if g>1.5*T)
    drop=sum(1 for r in rs if r['status'] in(2,3))
    late=sum(1 for x in w if x>T+pct(w,0.02))
    row=dict(n=len(P),e50=pct(e,.5),e95=pct(e,.95),e99=pct(e,.99),w50=pct(w,.5),w95=pct(w,.95),wmin=min(w) if w else 0,late=100*late/max(1,len(w)),rep=100*rep/max(1,len(gaps)),drop=100*drop/max(1,len(rs)),T=T)
    out[arm]=row
    print(f"{'lock' if arm else 'off':6}{row['n']:7d}{row['e50']:19.1f}{row['e95']:7.1f}{row['e99']:7.1f}{row['w50']:18.1f}{row['w95']:7.1f}{row['late']:7.2f}{row['rep']:9.2f}{row['drop']:9.2f}{100*len(e)/max(1,len(P)):8.1f}")
segp=[]
for arm,rs in segs:
    t0=rs[0]['ready_us'];P=[r for r in rs if r['status']==1 and r['bar_ok'] and r['ready_us']>=t0+skip*1e6]
    e=sorted((((r['present_us']+wmm+offs(r['present_us']+wmm))%(1<<32)-r['bar'])%(1<<32))/1000 for r in P)
    if len(e)>50: segp.append(('L' if arm else 'o')+'%.0f'%e[len(e)//2])
print('per-segment render->glass p50:',' '.join(segp))
print('refresh %.3f ms, min ready->glass %.1f ms'%(out[min(out)]['T'],out[min(out)]['wmin']))
if len(out)==2:
    a,b=out[0],out[1];print(f"GAIN lock vs off: render->glass p50 {a['e50']-b['e50']:+.1f} p95 {a['e95']-b['e95']:+.1f} p99 {a['e99']-b['e99']:+.1f} ms; ready->glass p50 {a['w50']-b['w50']:+.1f} p95 {a['w95']-b['w95']:+.1f} ms; repeats {a['rep']:.2f}% -> {b['rep']:.2f}%; dropped {a['drop']:.2f}% -> {b['drop']:.2f}%")
