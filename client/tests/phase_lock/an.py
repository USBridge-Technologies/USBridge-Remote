import json,sys,statistics as st
for fn in sys.argv[1:]:
    r=json.load(open(fn));f=r['frames']
    off=[(x['rx_us']-x['pts_us'])/1000 for x in f];b=min(off);d=sorted(o-b for o in off)
    n=len(d);h=sorted(x['host_ms'] for x in f)
    pd=sorted((b_['pts_us']-a['pts_us'])/1000 for a,b_ in zip(f,f[1:]))
    print(fn,'frames',n,'size p50',st.median(x['size'] for x in f),'host p50 %.1f p99 %.1f | rx-pts rel p50 %.1f p90 %.1f p99 %.1f max %.1f | pts-delta p50 %.1f p99 %.1f neg %d'%(h[n//2],h[int(n*.99)],d[n//2],d[int(n*.9)],d[int(n*.99)],d[-1],pd[len(pd)//2],pd[int(len(pd)*.99)],sum(1 for x in pd if x<=0)))
