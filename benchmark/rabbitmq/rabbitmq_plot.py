import matplotlib.pyplot as plt
import numpy as np 
import csv
import sys


def load_data(filename):
    data = {}
    with open(filename) as f:
        line = f.readline()
        while line:
            line = line.strip()
            if "sending rate avg" in line:
                data["Sending"] = int(line.split(" ")[5])
            if "receiving rate avg" in line:
                data["Receiving"] = int(line.split(" ")[5])
            line = f.readline()
    return data

BAR_WIDTH=0.25

labels=['Sending', 'Receiving']

data_num = len(sys.argv)-2 
factor = (data_num+1) * BAR_WIDTH

datas = []
for i in range(0, data_num):
    filename = sys.argv[1+i]
    data = load_data(filename)
    datas.append(data)

fig = plt.figure()
ax1 = fig.add_subplot()
ax1.set_ylabel("messages / second")

for i in range(0, data_num):
    filename = sys.argv[1+i]
    ax1.bar([BAR_WIDTH*i, factor + BAR_WIDTH*i], [datas[i][labels[0]], datas[i][labels[0]]], align="edge",  edgecolor="black", linewidth=1, width=BAR_WIDTH, label=filename)

h1, l1 = ax1.get_legend_handles_labels()
ax1.legend(h1, l1, loc="upper left")
plt.xlim(0, (len(labels)-1)*factor+BAR_WIDTH*data_num)
plt.xticks([x*factor+BAR_WIDTH*data_num/2 for x in range(0, len(labels))], labels)

plt.savefig(sys.argv[1+data_num])
