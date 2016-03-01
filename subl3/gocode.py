import sublime, sublime_plugin, subprocess

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
			result.append([arg[1] + "\t" + arg[0], arg[1]])

		return (result, sublime.INHIBIT_WORD_COMPLETIONS)
