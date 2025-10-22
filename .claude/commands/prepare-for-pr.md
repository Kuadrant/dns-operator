---
allowed-tools: Bash(git add:*), Bash(git status:*), Bash(git commit:*), Bash(make *)
description: Check that all generators have run and commit their changes
---


rebuild for helm, generate the CRD files, update the bundle, ammend all the changes to the current commit and verify that when rerun nothing new needs to be commited.