import matplotlib.pyplot as plt
import numpy as np 
import json
import sys


BAR_WIDTH=0.25

def load_data(filename):
    data = {}
    with open(filename) as f:
        line = f.readline()
        while line:
            # only single thread
            l_json = json.loads(line)[0]
            l_json["th_gbps"] = l_json["totalSize"] * 8 / l_json["totalElapsedSecond"] / 1024 / 1024 / 1024
            file = l_json["url"].split("/")[3]
            data[file] = l_json
            line = f.readline()
    return data

labels=['blk-1k', 'blk-32k', 'blk-512k', 'blk-1m', 'blk-32m', 'blk-128m', 'blk-512m', 'blk-1g']

plt.ylabel("Gbps")

data_num = len(sys.argv)-2 
factor = (data_num+1) * BAR_WIDTH
for i in range(0, data_num):
    filename = sys.argv[1+i]
    data = load_data(filename)       
    value = []
    for l in labels:
        value.append(data[l]["th_gbps"])
    plt.bar([x*factor+(BAR_WIDTH*i) for x in range(0, len(labels))], value, align="edge",  edgecolor="black", linewidth=1, width=BAR_WIDTH, label=filename)

plt.legend()
plt.xlim(0, (len(labels)-1)*factor+BAR_WIDTH*data_num)
plt.xticks([x*factor+BAR_WIDTH*data_num/2 for x in range(0, len(labels))], labels)

plt.savefig(sys.argv[1+data_num])

