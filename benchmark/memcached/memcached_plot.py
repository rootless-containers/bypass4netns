import matplotlib.pyplot as plt
import numpy as np 
import json
import sys


BAR_WIDTH=0.4

def load_data(filename):
    data = {}
    with open(filename) as f:
        d = json.load(f)
        if d == None:
            raise Exception("{} has invalid json format".format(filename))
        data["Ops/sec"] = []
        data["Ops/sec"].append(d["ALL STATS"]["Sets"]["Ops/sec"])
        data["Ops/sec"].append(d["ALL STATS"]["Gets"]["Ops/sec"])
        data["Latency"] = []
        data["Latency"].append(d["ALL STATS"]["Sets"]["Latency"])
        data["Latency"].append(d["ALL STATS"]["Gets"]["Latency"])
        return data

labels=['Sets(Ops/sec)', 'Gets(Ops/sec)', 'Sets(Latency)', 'Gets(Latency)']

plt.ylabel("Ops / seconds")

data_num = len(sys.argv)-2 
factor = (data_num+1) * BAR_WIDTH

datas = []
for i in range(0, data_num):
    filename = sys.argv[1+i]
    data = load_data(filename)
    datas.append(data)

fig = plt.figure()
ax1 = fig.add_subplot()
ax1.set_ylabel("Operations / second")
ax2 = ax1.twinx()
ax2.set_ylabel("latency (ms)")

for i in range(0, data_num):
    filename = sys.argv[1+i]
    ax1.bar([BAR_WIDTH*i, factor+BAR_WIDTH*i], datas[i]["Ops/sec"], align="edge",  edgecolor="black", linewidth=1, width=BAR_WIDTH, label=filename)

for i in range(0, data_num):
    filename = sys.argv[1+i]
    ax2.bar([factor*2+BAR_WIDTH*i, factor*3+BAR_WIDTH*i], datas[i]["Latency"], align="edge",  edgecolor="black", linewidth=1, width=BAR_WIDTH, label=filename)

h1, l1 = ax1.get_legend_handles_labels()
ax1.legend(h1, l1, loc="upper left")
plt.xlim(0, (len(labels)-1)*factor+BAR_WIDTH*data_num)
plt.xticks([x*factor+BAR_WIDTH*data_num/2 for x in range(0, len(labels))], labels)

plt.savefig(sys.argv[1+data_num])
