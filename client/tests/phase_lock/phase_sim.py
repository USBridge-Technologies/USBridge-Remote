# Simulates display-wait from measured frame arrival times.
# ready = rx (arrival) + decode; display latches at fixed grid of period T.
# free-run: averaged over all phases/drift. locked: best constant shift of capture, with miss budget.
import json,sys,statistics as st
def load(fn):
    f=json.load(open(fn))['frames']
    # first half dropped when backlog-free not guaranteed
    off=[(x['rx_us']-x['pts_us'])/1000 for x in f]
    b=min(off)
    return sorted(o-b for o in off), [o-b for o in off], f
def analyse(fn,T,dec=0.0,label=''):
    srt,off,f=load(fn)
    n=len(off)
    pct=lambda q:srt[min(n-1,int(n*q))]
    # free run: ideal evenly distributed wait + mean spread is latency we don't change
    free_mean=T/2; free_p95=T*0.95
    print(f"{label or fn}: frames {n} arrival spread p50 {pct(.5):.1f} p90 {pct(.9):.1f} p99 {pct(.99):.1f} max {srt[-1]:.1f} ms; T={T:.2f}")
    # for grid at pts-locked phase: frame i latches at pts+L+kT, with L the chosen constant arrival budget.
    for budget in (0.01,0.05):
        L=pct(1-budget)            # budget of frames allowed to miss
        wait=[(L-o) if o<=L else (L-o+T) for o in off]
        total=[o+w for o,w in zip(off,wait)]
        # free-run reference over phases: same arrivals, grid at phase p
        refs=[]
        for p in range(0,20):
            ph=T*p/20
            tt=[]
            for o in off:
                k=(o-ph)/T; import math
                tt.append(ph+math.ceil(k)*T)
            refs.append((st.mean(tt),sorted(tt)[int(n*.95)]))
        rm=st.mean(r[0] for r in refs); rp=st.mean(r[1] for r in refs)
        print(f"  miss<= {budget*100:.0f}%: locked arrival->screen mean {st.mean(total):.1f} p95 {sorted(total)[int(n*.95)]:.1f} | free-run mean {rm:.1f} p95 {rp:.1f} | gain mean {rm-st.mean(total):.1f} p95 {rp-sorted(total)[int(n*.95)]:.1f} ms")
for a in sys.argv[1:]:
    fn,hz=a.split(':'); analyse(fn,1000/float(hz))
