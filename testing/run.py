#!/usr/bin/env python
# coding=utf-8

import os, glob, subprocess, sys

total = 0
ok = 0
fail = 0

RED = "\033[0;31m"
GREEN = "\033[0;32m"
NC = "\033[0m"

OK = GREEN + "PASS!" + NC
FAIL = RED + "FAIL!" + NC

def run_test(t):
	global total, ok, fail
	total += 1
	c = glob.glob(t + "/cursor.*")[0]
	cursorpos = os.path.splitext(c)[1][1:]
	try:
		with open(t + "/out.expected", "r") as f:
			outexpected = f.read()
	except:
		outexpected = "To be determined"
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

if len(sys.argv) == 2:
	run_test(sys.argv[1])
else:
	for t in sorted(glob.glob("test.*")):
		run_test(t)

print "\nSummary (total: %d):" % total
print GREEN + "  PASS" + NC + ": %d" % ok
print RED +"  FAIL" + NC + ": %d" % fail

if fail == 0:
	print GREEN + "████████████████████████████████████████████████████████████████████" + NC
else:
	print RED + "████████████████████████████████████████████████████████████████████" + NC
