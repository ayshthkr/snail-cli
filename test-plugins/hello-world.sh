#!/bin/bash
# @name: hello-world
# @description: A simple hello world plugin
# @version: 1.0.0
# @author: Git-Mail Team
# @usage: hello-world [name]
# @example: hello-world Alice
# @example: hello-world

name=${1:-World}
echo "Hello, $name!"
echo "Repository: $GITMAIL_REPO_PATH"
echo "Working Directory: $GITMAIL_WORKING_DIR"

if [ -n "$GITMAIL_SELECTED_EMAILS" ]; then
    echo "Selected emails: $GITMAIL_SELECTED_EMAILS"
fi

if [ -n "$GITMAIL_CURRENT_FOLDER" ]; then
    echo "Current folder: $GITMAIL_CURRENT_FOLDER"
fi