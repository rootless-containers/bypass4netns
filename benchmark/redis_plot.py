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

labels_for_data=['PING_INLINE', 'PING_MBULK', 'SET', 'GET', 'INCR', 'LPUSH', 'RPUSH', 'LPOP', 'RPOP', 'SADD', 'HSET', 'SPOP', 'ZADD', 'ZPOPMIN', 'LPUSH (needed to benchmark LRANGE)', 'LRANGE_100 (first 100 elements)', 'LRANGE_300 (first 300 elements)', 'LRANGE_500 (first 500 elements)', 'LRANGE_600 (first 600 elements)', 'MSET (10 keys)', 'XADD']
labels=['PING\n_INLINE', 'PING\n_MBULK', 'SET', 'GET', 'INCR', 'LPUSH', 'RPUSH', 'LPOP', 'RPOP', 'SADD', 'HSET', 'SPOP', 'ZADD', 'ZPOPMIN', 'LPUSH', 'LRANGE\n_100', 'LRANGE\n_300', 'LRANGE\n_500', 'LRANGE\n_600', 'MSET\n(10 keys)', 'XADD']

plt.rcParams["figure.figsize"] = (20,4)
plt.ylabel("Request / seconds")

data_num = len(sys.argv)-2 
factor = (data_num+1) * BAR_WIDTH
for i in range(0, data_num):
    filename = sys.argv[1+i]
    data_csv = load_data(filename)
    value = []
    for l in labels_for_data:
        value.append(data_csv[l])
    plt.bar([x*factor+(BAR_WIDTH*i) for x in range(0, len(labels))], value, align="edge",  edgecolor="black", linewidth=1, width=BAR_WIDTH, label=filename)

plt.legend()
plt.xlim(0, (len(labels)-1)*factor+BAR_WIDTH*data_num)
plt.xticks([x*factor+BAR_WIDTH*data_num/2 for x in range(0, len(labels))], labels)

plt.savefig(sys.argv[1+data_num])
