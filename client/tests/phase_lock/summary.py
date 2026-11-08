import bisect,sys
from statistics import mean
def load(tag,skip=4):
    L=open(tag+'.csv').read().splitlines();w=int(L[0].split('wall_minus_mono_us=')[1].split()[0])
    R=[dict(zip(L[1].split(','),map(int,l.split(',')))) for l in L[2:]]
    C=sorted(tuple(map(float,l.split())) for l in open(tag+'.clock') if len(l.split())==2);T=[c[0] for c in C]
    def off(x):
        t=x/1e6;i=bisect.bisect(T,t)
        if i<=0:return C[0][1],0
        if i>=len(C):return C[-1][1],0
        a,b=C[i-1],C[i];return a[1]+(b[1]-a[1])*(t-a[0])/(b[0]-a[0]),abs(b[1]-a[1])
    segs=[]
    for r in R:
        if not segs or segs[-1][0]!=r['arm']:segs.append((r['arm'],[]))
        segs[-1][1].append(r)
    res={0:[],1:[]};flag={0:0,1:0}
    for a,rs in segs:
        t0=rs[0]['ready_us'];P=[r for r in rs if r['status']==1 and r['ready_us']>=t0+skip*1e6 and r['bar_ok']]
        if len(P)<100:continue
        e=[];bad=False
        for r in P:
            x=r['present_us']+w;o,j=off(x)
            # a clock step inside a segment makes its absolute numbers meaningless
            if j>2000:bad=True
            e.append(((x+o)%(1<<32)-r['bar'])%(1<<32))
        if bad:flag[a]+=1;continue
        e=sorted(v/1000 for v in e);g=[(q['present_us']-p['present_us'])/1000 for p,q in zip(P,P[1:])]
        res[a].append((mean(e),e[int(len(e)*.95)],100*sum(1 for v in g if v>25)/len(g)))
    return res,flag
print(f"{'run':8}{'off mean':>10}{'range':>12}{'lock mean':>11}{'range':>12}{'off rep%':>10}{'lock rep%':>10}{'gain':>7} seg_skipped")
for t in sys.argv[1:]:
    r,f=load(t);o,l=r[0],r[1]
    fm=lambda s:(f"{mean(x[0] for x in s):10.1f}{min(x[0] for x in s):6.0f}-{max(x[0] for x in s):<5.0f}" if s else ' '*22)
    print(f"{t:8}{fm(o)}{fm(l)}{mean(x[2] for x in o) if o else 0:10.2f}{mean(x[2] for x in l) if l else 0:10.2f}{(mean(x[0] for x in o)-mean(x[0] for x in l)) if o and l else 0:7.1f} {f}")
