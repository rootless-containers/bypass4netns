import argparse
import socket
import time

parser = argparse.ArgumentParser()
parser.add_argument("-s", "--server", help="run as server", action='store_true')
parser.add_argument("-c", "--client", help="run as client", action='store_true')
parser.add_argument("--host-ip", help="host ip address")
parser.add_argument("--netns-ip", help="netns ip address")
parser.add_argument("-p", "--port", help="bind or connect port")
parser.add_argument("-u", "--udp", help="use UDP", action='store_true')
parser.add_argument("--count", help="try count", default=1)
args = parser.parse_args()


TEST_MESSAGE = 'bypass4netns OK'

def server_tcp(args):
    print('test server starting...')
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, True)
    sock.settimeout(3.0)
    sock.bind(('0.0.0.0', int(args.port)))
    sock.listen()

    cnt = 0
    while cnt < int(args.count):
        con, _ = sock.accept()
        print('connection accepted.')
        recvline = con.recv(8192).decode()
        if recvline == TEST_MESSAGE:
            print('TEST_MESSAGE has been received.')
            cnt += 1
        con.close()
    sock.close()


def client_tcp(args):
    print('test client starting...')
    for _ in (0, int(args.count)):
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.settimeout(3.0)
        print('send TEST_MESSAGE to host')
        sock.connect((args.host_ip, int(args.port)))
        sock.sendmsg([TEST_MESSAGE.encode('utf-8')], [], 0, (args.host_ip, int(args.port)))
        sock.close()

        time.sleep(1.0)

        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.settimeout(3.0)
        print('send TEST_MESSAGE to netns')
        sock.connect((args.netns_ip, int(args.port)))
        sock.sendmsg([TEST_MESSAGE.encode('utf-8')], [], 0, (args.netns_ip, int(args.port)))
        sock.close()
    print('done.')

def server_udp(args):
    print('test server starting...')
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, True)
    sock.settimeout(3.0)
    sock.bind(('0.0.0.0', int(args.port)))

    cnt = 0
    while cnt < int(args.count):
        recvline = sock.recv(8192).decode()
        if recvline == TEST_MESSAGE:
            print('TEST_MESSAGE has been received.')
            cnt += 1
    sock.close()


def client_udp(args):
    print('test client starting...')
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.settimeout(3.0)
    for _ in (0, int(args.count)):
        print('send TEST_MESSAGE to host')
        sock.sendmsg([TEST_MESSAGE.encode('utf-8')], [], 0, (args.host_ip, int(args.port)))

        time.sleep(1.0)

        print('send TEST_MESSAGE to netns')
        sock.sendmsg([TEST_MESSAGE.encode('utf-8')], [], 0, (args.netns_ip, int(args.port)))
    sock.close()
    print('done.')

if __name__ == '__main__':
    if args.udp:
        if args.server:
            server_udp(args)
        elif args.client:
            client_udp(args)
    else:
        if args.server:
            server_tcp(args)
        elif args.client:
            client_tcp(args)
