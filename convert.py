import glob
import os
import time
from concurrent.futures import ProcessPoolExecutor


def merge():
    dirs = sorted(
        [
            os.path.join("/data", d)
            for d in os.listdir("/data")
            if os.path.isdir(os.path.join("/data", d)) and d.startswith("manifest")
        ],
        key=os.path.getctime,
    )
    if len(dirs) == 0:
        print("No manifest is exported!!!")
        return
    manifest = dirs[-1]

    path = os.path.join(manifest, "by-filepath", "clinical", "tsv")

    if os.path.isfile(manifest + ".sync"):
        print("The manifest is already converted")
        return

    output_tsv_folder = os.path.join("/data", manifest.replace("manifest", "tsv"))

    if not os.path.isdir(output_tsv_folder):
        os.mkdir(output_tsv_folder)

    print("Getting the latest manifest")
    print(path)
    for tbl_name in ["ActionableMutations", "ICDCode", "Oncology_Primary", "Patients"]:
        print(tbl_name)
        if not os.path.isdir(os.path.join(path, tbl_name)):
            print("Mounting not finished yet")
            return

        filenames = glob.glob(os.path.join(path, tbl_name, "*.tsv"))

        with open(
            os.path.join(output_tsv_folder, "{}.tsv".format(tbl_name)), "w"
        ) as outfile:
            with ProcessPoolExecutor(max_workers=10) as pool:
                result = pool.map(readfile, filenames)
                count = 0
                for r in result:
                    if count % 100 == 0:
                        print("Processed {} {}".format(count, tbl_name))
                    if not count:
                        outfile.writelines(r)
                    else:
                        outfile.writelines(r[1:])
                    count += 1
    with open(manifest + ".sync", "w") as f:
        f.write("Done")


def readfile(filename):
    retries = 0
    while True:
        try:
            with open(filename, "r") as f:
                return f.readlines()
        except Exception as e:
            print("Fail to read file {}, retry".format(str(e)))
            time.sleep(5)
            retries += 1
            if retries > 5:
                print("Fail to get file {}".format(filename))
                return []


if __name__ == "__main__":
    while True:
        merge()
        time.sleep(1)
