import sublime, sublime_plugin, subprocess

# go to balanced pair, e.g.:
# ((abc(def)))
# ^
# \--------->^
#
# returns -1 on failure
def skip_to_balanced_pair(str, i, open, close):
	count = 1
	i += 1
	while i < len(str):
		if str[i] == open:
			count += 1
		elif str[i] == close:
			count -= 1

		if count == 0:
			break
		i += 1
	if i >= len(str):
		return -1
	return i

# split balanced parens string using comma as separator
# e.g.: "ab, (1, 2), cd" -> ["ab", "(1, 2)", "cd"]
# filters out empty strings
def split_balanced(s):
	out = []
	i = 0
	beg = 0
	while i < len(s):
		if s[i] == ',':
			out.append(s[beg:i].strip())
			beg = i+1
			i += 1
		elif s[i] == '(':
			i = skip_to_balanced_pair(s, i, "(", ")")
			if i == -1:
				i = len(s)
		else:
			i += 1

	out.append(s[beg:i].strip())
	return list(filter(bool, out))


def extract_arguments_and_returns(sig):
	sig = sig.strip()
	if not sig.startswith("func"):
		return [], []

	# find first pair of parens, these are arguments
	beg = sig.find("(")
	if beg == -1:
		return [], []
	end = skip_to_balanced_pair(sig, beg, "(", ")")
	if end == -1:
		return [], []
	args = split_balanced(sig[beg+1:end])

	# find the rest of the string, these are returns
	sig = sig[end+1:].strip()
	if sig.startswith("("):
		sig = sig[1:]
	if sig.endswith(")"):
		sig = sig[:-1]
	returns = split_balanced(sig)

	return args, returns

# takes gocode's candidate and returns sublime's hint and subj
def hint_and_subj(cls, name, type):
	subj = name
	hint = subj + "\t" + cls
	args, returns = extract_arguments_and_returns(type)
	if returns:
		hint = subj + "\t" + ", ".join(returns)
	if not args and cls == "func":
		subj += "()"
	if args:
		sargs = []
		for i, a in enumerate(args):
			ea = a.replace("{", "\\{").replace("}", "\\}")
			sargs.append("${{{0}:{1}}}".format(i+1, ea))
		subj += "(" + ", ".join(sargs) + ")"
	return hint, subj

class Gocode(sublime_plugin.EventListener):
	def on_query_completions(self, view, prefix, locations):
		loc = locations[0]
		if not view.match_selector(loc, "source.go"):
			return None

		src = view.substr(sublime.Region(0, view.size()))
		filename = view.file_name()
		cloc = "c{0}".format(loc)
		gocode = subprocess.Popen(["gocode", "-f=csv", "autocomplete", filename, cloc],
			stdin=subprocess.PIPE, stdout=subprocess.PIPE)
		out = gocode.communicate(src.encode())[0].decode()

		result = []
		for line in filter(bool, out.split("\n")):
			arg = line.split(",,")
			hint, subj = hint_and_subj(*arg)
			result.append([hint, subj])

		return (result, sublime.INHIBIT_WORD_COMPLETIONS)
