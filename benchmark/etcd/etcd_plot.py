import matplotlib.pyplot as plt
import numpy as np 
import csv
import sys


def load_data(filename):
    data = {}
    with open(filename) as f:
        line = f.readline()
        while line:
            line = line.strip().replace("\t", " ")
            if "Requests/sec" in line:
                data["Requests/sec"] = float(line.split(" ")[1])
            if "Average" in line:
                data["Latency(ms)"] = float(line.split(" ")[1]) * 1000
            line = f.readline()
    return data

BAR_WIDTH=0.25

labels=['Requests/sec', 'Latency(ms)']

data_num = len(sys.argv)-2 
factor = (data_num+1) * BAR_WIDTH

datas = []
for i in range(0, data_num):
    filename = sys.argv[1+i]
    data = load_data(filename)
    datas.append(data)


fig = plt.figure()
ax1 = fig.add_subplot()
ax1.set_ylabel("Requests / second")
ax2 = ax1.twinx()
ax2.set_ylabel("Average latency (ms)")

for i in range(0, data_num):
    filename = sys.argv[1+i]
    ax1.bar([BAR_WIDTH*i], datas[i][labels[0]], align="edge",  edgecolor="black", linewidth=1, width=BAR_WIDTH, label=filename)

for i in range(0, data_num):
    filename = sys.argv[1+i]
    ax2.bar([factor+BAR_WIDTH*i], datas[i][labels[1]], align="edge",  edgecolor="black", linewidth=1, width=BAR_WIDTH, label=filename)

h1, l1 = ax1.get_legend_handles_labels()
ax1.legend(h1, l1, loc="upper left")
plt.xlim(0, (len(labels)-1)*factor+BAR_WIDTH*data_num)
plt.xticks([x*factor+BAR_WIDTH*data_num/2 for x in range(0, len(labels))], labels)

plt.savefig(sys.argv[1+data_num])
