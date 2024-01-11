import matplotlib.pyplot as plt
import numpy as np 
import json
import sys


BAR_WIDTH=0.4

def load_data(filename):
    with open(filename) as f:
        return json.load(f)

labels=['sum_received.bits_per_second']

#plt.rcParams["figure.figsize"] = (20,4)
plt.ylabel("Gbps")

data_num = len(sys.argv)-2 
factor = (data_num+1) * BAR_WIDTH
for i in range(0, data_num):
    filename = sys.argv[1+i]
    data_json = load_data(filename)       
    value = [data_json["end"]["sum_received"]["bits_per_second"] / 1024 / 1024 / 1024]
    plt.bar([x*factor+(BAR_WIDTH*i) for x in range(0, len(labels))], value, align="edge",  edgecolor="black", linewidth=1, width=BAR_WIDTH, label=filename)

plt.legend()
plt.xlim(0, (len(labels)-1)*factor+BAR_WIDTH*data_num)
plt.xticks([x*factor+BAR_WIDTH*data_num/2 for x in range(0, len(labels))], labels)

plt.savefig(sys.argv[1+data_num])
