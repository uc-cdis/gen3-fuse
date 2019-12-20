import glob
import os
import time
from concurrent.futures import ProcessPoolExecutor
import zipfile


def merge():
    dirs = sorted([os.path.join('/data', d) for d in os.listdir('/data') if os.path.isdir(os.path.join('/data', d)) and d != 'data' and "_merged" not in d])
    if len(dirs) == 0:
        print('no manifest is exported')
        return
    manifest = dirs[-1]
    
    path = os.path.join(manifest, 'by-filepath', 'clinical', 'archive')
    
    if os.path.isfile(manifest+'.sync'):
        print("already converted")
        return
    print('getting latest manifest')    
    print(path)

    merged_path = manifest + "_merged"

    with zipfile.ZipFile("headers.zip") as z:
        z.extractall(path=merged_path)
    
    if not os.path.isdir(path):
        print("mounting not finished yet")
        return

    filenames = glob.glob(os.path.join(path, '*.zip'))

    for filename in filenames:
        with zipfile.ZipFile(filename) as z:
            for f in z.namelist():
                with z.open(f) as zipped_file:
                    data = zipped_file.read().decode()

                merged_file = f.split('/')[1]

                with open(os.path.join(merged_path, merged_file), "a") as output:
                    output.write(data)

    # for tbl_name in ['ActionableMutations', 'ICDCode', 'Oncology_Primary', 'Patients']:
    #     print(tbl_name)
    #     if not os.path.isdir(os.path.join(path, tbl_name)):
    #         print("mounting not finished yet")
    #         return
        
    #     filenames = glob.glob(os.path.join(path, tbl_name, '*.tsv'))

    #     with open(os.path.join('/data', '{}.tsv'.format(tbl_name)), 'w') as outfile:
    #         with ProcessPoolExecutor(max_workers=10) as pool:
    #             result = pool.map(readfile, filenames)
    #             count = 0
    #             first = True
    #             for r in result:
    #                 if count % 100 == 0:
    #                     print("Processed {} {}".format(count, tbl_name))
    #                 count += 1
    #                 if first:
    #                     outfile.writelines(r)
    #                     first = False
    #                 else:
    #                     outfile.writelines(r[1:])
    with open(manifest+'.sync', 'w') as f:
        f.write("finished")      

def readfile(filename):
    retries = 0
    while True:
        try:
            with open(filename, 'r') as f:
                return f.readlines()
        except Exception as e:
            print("Fail to read file {}, retry".format(str(e)))
            time.sleep(5)
            retries += 1
            if retries > 5:
                print("Fail to get file {}".format(filename))
                return []

if __name__ == "__main__":
    while(True):
        merge()
        time.sleep(1)
