import socket,struct,time
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);s.bind(('0.0.0.0',47900))
while True:
    d,a=s.recvfrom(64);s.sendto(struct.pack('d',time.time()),a)
