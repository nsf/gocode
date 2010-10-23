#!/usr/bin/env python2
# coding=utf-8

import os, glob, subprocess, sys, json, optparse, platform

RED = "\033[0;31m"
GREEN = "\033[0;32m"
YELLOW = "\033[0;33m"
MAGENTA = "\033[0;35m"
NC = "\033[0m"

OK = GREEN + "PASS!" + NC
FAIL = RED + "FAIL!" + NC
EXPECTED = YELLOW + "EXPECTED: " + NC
LINE = "──────────────────────────────────────────────────────"

class Testing(object):
	def __init__(self):
		self.total = 0
		self.ok = 0
		self.fail = 0
		self.expected_fail = 0
		self.compiler = ""

	def print_summary(self):
		print "\nSummary (total: %d):" % self.total
		print GREEN + "  PASS" + NC + ": %d" % self.ok
		print RED +"  FAIL" + NC + ": %d (unexpected failures)" % self.fail

		if self.fail == 0:
			print GREEN + "████████████████████████████████████████████████████████████████████" + NC
		else:
			print RED + "████████████████████████████████████████████████████████████████████" + NC

t = Testing()

# name of the test + commentary (why it is expected to fail)
expected_to_fail = {}

def load_json(filename):
	with open(filename, "r") as f:
		return json.load(f)

def load_file(filename):
	with open(filename, "r") as f:
		return f.read()

def check_source(filename):
	gc = subprocess.Popen("%s -o tmp.obj %s" % (t.compiler, filename), shell=True,
			stdout=subprocess.PIPE)
	if gc.wait() == 0:
		return True
	else:
		return False

def listidents(filename):
	listidents = subprocess.Popen("../listidents %s" % filename, shell=True,
			stdout=subprocess.PIPE)
	return json.loads(listidents.communicate()[0])

def gocode_smap(filename):
	smap = subprocess.Popen("gocode smap %s" % filename, shell=True,
			stdout=subprocess.PIPE)
	return json.loads(smap.communicate()[0])

def check_smap(idents, smap):
	for i in idents:
		found = False
		for e in smap:
			if i["Offset"] == e["Offset"]:
				found = True
				break

		if not found:
			return False
	return True

def run_test(test):
	t.total += 1
	print MAGENTA + "Processing %s..." % test + NC

	src = test + "/test.go"

	# 1. Source code check
	sys.stdout.write("Initial source code check... ")
	sys.stdout.flush()
	if check_source(src):
		print OK
	else:
		print FAIL
		t.fail += 1
		return

	# 2. SMap check
	sys.stdout.write("Checking semantic map completeness... ")
	sys.stdout.flush()
	smap = gocode_smap(src)
	idents = listidents(src)
	if check_smap(idents, smap):
		print OK
	else:
		print FAIL
		t.fail += 1
		print LINE
		os.system("../showsmap %s" % src)
		print LINE
		return

	# 3. Rename each identifier and check compilation
	sys.stdout.write("Renaming check... ")
	sys.stdout.flush()
	for n, i in enumerate(idents):
		sys.stdout.write("%d%%... " % (float(n)/len(idents) * 100))
		sys.stdout.flush()
		if os.system("../rename %s %s RenamedIdent123 > tmp.go" % (src, i["Offset"])) != 0:
			print FAIL
			t.fail += 1
			print LINE
			os.system("../showcursor %s %s" % (src, i["Offset"]))
			print LINE
			return
		if not check_source("tmp.go"):
			print FAIL
			t.fail += 1
			print LINE
			os.system("../showcursor %s %s" % (src, i["Offset"]))
			print LINE
			os.system("cat tmp.go")
			print LINE
			return
		sys.stdout.write("\r                                        \rRenaming check... ")
	print OK

	t.ok += 1

	#gofile = load_file(test + "/test.go")
	#cases = load_json(test + "/cases.json")
"""
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
		if t in expected_to_fail:
			print t + ": " + FAIL + " " + EXPECTED + expected_to_fail[t]
			expected_fail += 1
		else:
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
"""

def get_default_compiler():
	goarch = os.getenv("GOARCH")

	if goarch == '386':
		machine = 'i386'
	elif goarch == 'amd64':
		machine = 'x86_64'
	elif goarch == 'arm':
		machine = 'arm'
	else:
		machine = platform.machine()

	if machine == 'x86_64':
		return "6g"
	elif machine in ['i386', 'i486', 'i586', 'i686']:
		return "8g"
	elif machine == 'arm':
		return "5g"

parser = optparse.OptionParser()
parser.add_option("-c", "--compiler", dest="compiler", default=get_default_compiler(),
		help="use this compiler to check sources for correctness")

(options, args) = parser.parse_args()

t.compiler = options.compiler
if len(args) == 1:
	run_test(args[0])
else:
	for test in sorted(glob.glob("test.*")):
		run_test(test)

t.print_summary()

try: os.unlink("tmp.go")
except: pass
try: os.unlink("tmp.obj")
except: pass

