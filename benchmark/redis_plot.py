import matplotlib.pyplot as plt
import numpy as np 
import csv
import sys


def load_data(filename):
    data = {}
    with open(filename) as f:
        reader = csv.reader(f)
        next(reader, None)
        for row in reader:
            data[row[0]] = float(row[1])
    return data

BAR_WIDTH=0.25


data_wo_b4ns_direct = load_data(sys.argv[1])
data_wo_b4ns_host = load_data(sys.argv[2])
data_w_b4ns = load_data(sys.argv[3])

labels_for_data=['PING_INLINE', 'PING_MBULK', 'SET', 'GET', 'INCR', 'LPUSH', 'RPUSH', 'LPOP', 'RPOP', 'SADD', 'HSET', 'SPOP', 'ZADD', 'ZPOPMIN', 'LPUSH (needed to benchmark LRANGE)', 'LRANGE_100 (first 100 elements)', 'LRANGE_300 (first 300 elements)', 'LRANGE_500 (first 500 elements)', 'LRANGE_600 (first 600 elements)', 'MSET (10 keys)', 'XADD']

value_wo_b4ns_direct = []
value_wo_b4ns_host = []
value_w_b4ns = []
for l in labels_for_data:
    value_wo_b4ns_direct.append(data_wo_b4ns_direct[l])
    value_wo_b4ns_host.append(data_wo_b4ns_host[l])
    value_w_b4ns.append(data_w_b4ns[l])

labels=['PING\n_INLINE', 'PING\n_MBULK', 'SET', 'GET', 'INCR', 'LPUSH', 'RPUSH', 'LPOP', 'RPOP', 'SADD', 'HSET', 'SPOP', 'ZADD', 'ZPOPMIN', 'LPUSH', 'LRANGE\n_100', 'LRANGE\n_300', 'LRANGE\n_500', 'LRANGE\n_600', 'MSET\n(10 keys)', 'XADD']
print(value_wo_b4ns_direct)
print(value_wo_b4ns_host)
print(value_w_b4ns)

 
plt.rcParams["figure.figsize"] = (20,4)

plt.ylabel("Request / seconds")
plt.bar([x for x in range(0, len(labels))], value_wo_b4ns_direct, align="edge",  edgecolor="black", linewidth=1, hatch='//', width=BAR_WIDTH, label='w/o bypass4netns(direct)')
plt.bar([x+BAR_WIDTH for x in range(0, len(labels))], value_wo_b4ns_host, align="edge",  edgecolor="black", linewidth=1, hatch='//', width=BAR_WIDTH, label='w/o bypass4netns(via host)')
plt.bar([x+BAR_WIDTH*2 for x in range(0, len(labels))], value_w_b4ns, align="edge", edgecolor="black", linewidth=1, hatch='++', width=BAR_WIDTH, label='w/ bypass4netns(via host)')

plt.legend()
plt.xlim(0, len(labels)+BAR_WIDTH*3-1)
plt.xticks([x+BAR_WIDTH*1.5 for x in range(0, len(labels))], labels)

plt.savefig(sys.argv[4])
