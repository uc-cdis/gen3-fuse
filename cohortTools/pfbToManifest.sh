#!/bin/bash

# Usage:
# ./pfbToManifest.sh <pfb-filename> <manifest-output-filename>

# This script takes an exported cohort's PFB and parses out object IDs corresponding data files
# in indexd. The object IDs are outputted in a new manifest.json file of the format
# [
#    {
#        "object_id" : "fbd5b74e-6789-4f42-b88f-f75e72777f5d",
#    },
#    ...
# ]

pfb_filename="$1"
manifest_output_filename="$2"

if [[ ! -f "$pfb_filename" ]]; then
    echo "Error: Input file $pfb_filename does not exist."
    exit 1
fi

if [[ -f "$manifest_output_filename" ]]; then
  rm "$manifest_output_filename"
fi
# mkdir before touch because GUIDs with prefix contain "/"
mkdir -p $(dirname $manifest_output_filename)
touch "$manifest_output_filename"

files=$(pfb show -i "$pfb_filename" | grep "object_id")

if [[ $? -ne 0 ]]; then
  echo "Parsing $pfb_filename failed. Exiting..."
  exit 1
fi

echo "[" >> "$manifest_output_filename"

while read -r record; do
  object_id=$(jq --raw-output .object.object_id <<< "$record")
  echo "{\"object_id\":\"$object_id\"}," >> "$manifest_output_filename"
done <<< "$files"

manifest_json=$(sed '$s/,$//' < "$manifest_output_filename")

echo "$manifest_json" > "$manifest_output_filename"

echo "]" >> "$manifest_output_filename"

trimmed_manifest_contents=$(cat "$manifest_output_filename" | tr -d " \t\n\r")

echo "$trimmed_manifest_contents" > "$manifest_output_filename"
