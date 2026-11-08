# NTP-style offset (mac wall - this wall), keeping the fastest round trips.
import socket,struct,sys,time
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);s.settimeout(0.3);best=[]
for i in range(120):
    t1=time.time();s.sendto(b'x',(__import__('os').environ['MAC_HOST'],47900))
    try:d,_=s.recvfrom(64)
    except Exception: continue
    t2=time.time();m=struct.unpack('d',d)[0];best.append((t2-t1,m-(t1+t2)/2));time.sleep(0.002)
best.sort();b=best[:15];print(time.time(), sum(x[1] for x in b)/len(b)*1e6, flush=True)
