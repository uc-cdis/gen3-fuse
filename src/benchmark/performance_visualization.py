#!/usr/bin/env python3

import re
import statsmodels.api as sm
import numpy as np
import matplotlib
import matplotlib.pyplot as plt
import math

def parse_times_file(filename):
    f = open(filename, "r")
    lines = f.readlines()
    f.close()

    # Get the filename of the manifest these results are for
    p = re.compile(".*(/.*\.json).*")
    manifest_filename = p.search(lines[0]).group(1)[1:]
    print("Timing results for " + manifest_filename)

    num_files = 0
    mount_time_pattern = re.compile(".*mount filesystem: ([0-9]+\.[0-9]*)")
    mount_times = []

    ls_time_pattern = re.compile(".*list ([0-9]+) files: ([0-9]+\.[0-9]*)")
    ls_times = []

    cat_time_pattern = re.compile(".*to cat .* of size ([0-9]+) bytes: ([0-9]+\.[0-9]*)")
    cat_times = []

    for line in lines[1:]:
        try:
            time_to_mount = float(mount_time_pattern.search(line).group(1))
            mount_times.append(time_to_mount)
        except Exception:
            pass

        try:
            time_to_ls = float(ls_time_pattern.search(line).group(2))
            ls_times.append(time_to_ls)
            num_files = float(ls_time_pattern.search(line).group(1))
        except Exception:
            pass

        try:
            # convert filesizes bytes -> megabytes
            filesize = float(cat_time_pattern.search(line).group(1)) / (1000000.0)
            # convert seconds to minutes
            time_to_cat = float(cat_time_pattern.search(line).group(2)) / 60.0
            cat_times.append((filesize, time_to_cat))
        except Exception:
            pass

    return num_files, mount_times, ls_times, cat_times, manifest_filename

def plot_file_size_performance_graph(cat_times):
    X = np.asarray([x[0] for x in cat_times])
    Y = np.asarray([ x[1] for x in cat_times])

    mpl_fig = plt.figure()
    ax = mpl_fig.add_subplot(111)
    dot_area = np.asarray([60 for x in cat_times])
    alphas = np.asarray([0.3 for x in cat_times])
    colors = np.asarray(['green' for x in cat_times])
    plt.scatter(X,Y, s=dot_area, marker='o', c=colors, edgecolors='black')
    plt.title('Gen3Fuse cat times by file size')
    plt.grid(True)
    ax.set_xlabel('filesize in MB')
    ax.set_ylabel('minutes to cat')

    plt.show()

def plot_num_files_mount_time_performance_graph(to_plot):
    x_arr = []
    y_arr = []
    for entry in to_plot:
        num_files = entry["num_files"]
        for time in entry["mount_times"]:
            x_arr.append(num_files)
            y_arr.append(time)

    X = np.asarray(x_arr)
    Y = np.asarray(y_arr)

    mpl_fig = plt.figure()
    ax = mpl_fig.add_subplot(111)
    dot_area = np.asarray([100 for x in x_arr])
    alphas = np.asarray([0.3 for x in x_arr])
    colors = np.asarray(['green' for x in x_arr])
    plt.scatter(X,Y, s=dot_area, marker='o', c=colors, edgecolors='black')
    plt.title('Gen3Fuse mount times by the number of files')
    plt.grid(True)
    ax.set_xlabel('number of files in manifest')
    ax.set_ylabel('seconds to mount')

    plt.show()

def plot_num_files_ls_time_performance_graph(to_plot):
    x_arr = []
    y_arr = []
    for entry in to_plot:
        num_files = entry["num_files"]
        for time in entry["ls_times"]:
            x_arr.append(num_files)
            y_arr.append(time)

    X = np.asarray(x_arr)
    Y = np.asarray(y_arr)

    mpl_fig = plt.figure()
    ax = mpl_fig.add_subplot(111)
    dot_area = np.asarray([100 for x in x_arr])
    alphas = np.asarray([0.3 for x in x_arr])
    colors = np.asarray(['green' for x in x_arr])
    plt.scatter(X,Y, s=dot_area, marker='o', c=colors, edgecolors='black')
    plt.title('Gen3Fuse ls times by the number of files')
    plt.grid(True)
    ax.set_xlabel('number of files in manifest')
    ax.set_ylabel('seconds to ls')

    plt.show()

# Cat times by file size
num_files, mount_times, ls_times, cat_times, manifest = parse_times_file("results/performance-file-sizes.txt")
plot_file_size_performance_graph(cat_times)

# ls and mount times by # of files
manifest_sizes = [10, 100, 1000, 2000, 5000, 10000]
to_plot = []
for i in range(len(manifest_sizes)):
    num_files, mount_times, ls_times, cat_times, manifest = parse_times_file("results/performance-{}-files.txt".format(str(manifest_sizes[i])))
    to_plot.append({
        "num_files" : num_files,
        "ls_times" : ls_times,
        "mount_times" : mount_times
    })

plot_num_files_mount_time_performance_graph(to_plot)
plot_num_files_ls_time_performance_graph(to_plot)
