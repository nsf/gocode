#!/usr/bin/env python
# coding=utf-8

import os, glob, subprocess

total = 0
ok = 0
fail = 0

RED = "\033[0;31m"
GREEN = "\033[0;32m"
NC = "\033[0m"

OK = GREEN + "PASS!" + NC
FAIL = RED + "FAIL!" + NC

for t in sorted(glob.glob("test.*")):
	total += 1
	c = glob.glob(t + "/cursor.*")[0]
	cursorpos = os.path.splitext(c)[1][1:]
	with open(t + "/out.expected", "r") as f:
		outexpected = f.read()
	filename = t + "/test.go"
	gocode = subprocess.Popen("gocode -in %s autocomplete %s %s" % (filename, filename, cursorpos),
			shell=True, stdout=subprocess.PIPE)
	out = gocode.communicate()[0]
	if out != outexpected:
		print t + ": " + FAIL
		print "--------------------------------------------------------"
		print "Got:\n" + out
		print "--------------------------------------------------------"
		print "Expected:\n" + outexpected
		print "--------------------------------------------------------"
		fail += 1
	else:
		print t + ": " + OK
		ok += 1

print "\nSummary (total: %d):" % total
print GREEN + "  PASS" + NC + ": %d" % ok
print RED +"  FAIL" + NC + ": %d" % fail

if fail == 0:
	print GREEN + "████████████████████████████████████████████████████████████████████" + NC
else:
	print RED + "████████████████████████████████████████████████████████████████████" + NC
