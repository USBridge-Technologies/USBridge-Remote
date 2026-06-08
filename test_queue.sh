#!/bin/bash
q=("a")
i=0
while [ $i -lt "${#q[@]}" ]; do
    echo "Processing ${q[$i]}"
    if [ $i -lt 3 ]; then
        q+=("b$i")
    fi
    i=$((i+1))
done
