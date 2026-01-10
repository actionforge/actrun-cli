echo "Test File Zip"
set -o pipefail
TEST_NAME=file-compress
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

list_files_and_sizes() {
    local directory="$1"
    if [[ "$OSTYPE" == "linux-gnu"* ]] || [[ "$OSTYPE" == "cygwin" ]] || [[ "$OSTYPE" == "msys" ]]; then
        find "$directory" -type f -exec stat --format="%n %s" {} +
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        find "$directory" -type f -exec stat -f "%N %z" {} +
    else
        echo "Unsupported OS"
        exit 1
    fi
}

mkdir -p compress_me/subdir/bar
echo "Hello Foo 1" > compress_me/file1.txt
echo "Hello Bar 12" > compress_me/subdir/foo1.txt
echo "Hello Bas 123" > compress_me/subdir/foo2.txt
echo "Hello Bak 1234" > compress_me/subdir/foo3.txt

# this folder is added to file-compress just with its name 'compress_me2',
# not the individual files, just to check recursive of file-compress works.
mkdir -p compress_me2/subdir/bar
echo "12" > compress_me2/a.txt
echo "abc" > compress_me2/subdir/b.txt
echo "abcd" > compress_me2/subdir/c.txt


echo "Hello Zip 123" > compress_me/subdir/bar/bar.txt
#! test actrun -- zip
#! test unzip -Z1 compressed.zip | sort
mkdir extracted_zip
#! test unzip compressed.zip -d extracted_zip | sort
#! test list_files_and_sizes extracted_zip | sort

echo "Hello tar 1234" > compress_me/subdir/bar/bar.txt
#! test actrun -- tar
#! test tar -tf compressed.tar | sort
#! test tar -xOf compressed.tar | sort
mkdir extracted_tar
# Send stderr to /dev/null to avoid the warning "tar: compressed.tar: time stamp 2021-09-30 15:00:00 is 0.12345 s in the future"
tar -xf compressed.tar -C extracted_tar 2>/dev/null
#! test list_files_and_sizes extracted_tar | sort

echo "Hello tar.gz" > compress_me/subdir/bar/bar.txt
#! test actrun -- targz
#! test tar -tzf compressed.tar.gz | sort
#! test tar -xzOf compressed.tar.gz | sort
mkdir extracted_targz
# Send stderr to /dev/null to avoid the warning "tar: compressed.tar: time stamp 2021-09-30 15:00:00 is 0.12345 s in the future"
tar -xzf compressed.tar.gz -C extracted_targz 2>/dev/null
#! test list_files_and_sizes extracted_targz | sort
