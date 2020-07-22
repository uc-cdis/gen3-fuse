#!/bin/bash

# Usage:
# ./pfbToManifest.sh <pfb-filename> <manifest-output-filename>

pfb_filename="$1"
manifest_output_filename="$2"

if [ ! -f "$pfb_filename" ]; then
    echo "Error: Input file $pfb_filename does not exist."
    exit 1
fi

> $manifest_output_filename

files=$(pfb show -i $pfb_filename | grep "object_id")
# files=$(pfb show -i $pfb_filename -n 70 | grep "object_id") # for testing purposes

echo "[" >> $manifest_output_filename

while read record; do
  object_id=$(jq --raw-output .object.object_id <<< $record)
  echo "{\"object_id\":\"$object_id\"}," >> $manifest_output_filename
done <<< "$files"

manifest_json=$(sed '$s/,$//' < $manifest_output_filename)

echo $manifest_json > $manifest_output_filename

echo "]" >> $manifest_output_filename

trimmed_manifest_contents=$(cat $manifest_output_filename | tr -d " \t\n\r")

echo $trimmed_manifest_contents > $manifest_output_filename