#!/bin/bash

# Extracts the versions of multiple dependencies from the go.mod file
# Usage: ./extract_dependency_versions.sh <dependency_name_1> <dependency_name_2> ...
# Example: ./extract_dependency_versions.sh github.com/my/dependency1 github.com/my/dependency2

# Loop through the provided dependencies and extract their versions
for dependency_name in "$@"; do
    # dependency_version=$(grep -oP "^$dependency_name v\K.*" go.mod | cut -d' ' -f1)
    dependency_version=$(go list -m -f "{{.Version}}" $dependency_name)
    env_variable=$(echo "$dependency_name" | tr '[:lower:]' '[:upper:]' | tr '/.-' '_')_VERSION
    export ${env_variable}="${dependency_version}"
done
