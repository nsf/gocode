#!/usr/bin/env ruby
# encoding: utf-8

require 'json'

RED = "\033[0;31m"
GRN = "\033[0;32m"
YLW = "\033[0;33m"
MGN = "\033[0;35m"
NC  = "\033[0m"

PASS = "#{GRN}PASS!#{NC}"
FAIL = "#{RED}FAIL!#{NC}"

Stats = Struct.new :total, :ok, :fail
$stats = Stats.new 0, 0, 0

def print_stats
	puts "\nSummary (total: #{$stats.total})"
	puts "#{GRN}  PASS#{NC}: #{$stats.ok}"
	puts "#{RED}  FAIL#{NC}: #{$stats.fail}"
	puts "#{$stats.fail == 0 ? GRN : RED}#{"█"*72}#{NC}"
end

$compiler = case ENV['GOARCH']
	when '386'   then '8g'
	when 'amd64' then '6g'
	when 'arm'   then '5g'
	else case RUBY_PLATFORM.split('-').first
		when /i[3-6]86/ then '8g'
		when 'x86_64'   then '6g'
		when 'arm'      then '5g'
	end
end

def check_source?(filename)
	# eat output here
	%x[#{$compiler} -o tmp.obj #{filename}]
	return $?.success?
end

def check_smap?(idents, smap)
	return idents.all? {|i| smap.any? {|e| i['Offset'] == e['Offset']}}
end

def run_test(t)
	$stats.total += 1
	puts "#{MGN}Processing #{t}...#{NC}"
	src = "#{t}/test.go"

	# 1. Source code check
	print "Initial source code check... "
	STDOUT.flush
	if check_source? src then
		puts PASS
	else
		puts FAIL
		$stats.fail += 1
		return
	end

	# 2. SMap check
	print "Checking semantic map completeness... "
	STDOUT.flush

	smap = JSON.parse(%x[gocode smap #{src}])
	idents = JSON.parse(%x[../listidents #{src}])

	if check_smap?(idents, smap) then
		puts PASS
	else
		puts FAIL
		$stats.fail += 1
		puts "─"*72
		system "../showsmap #{src}"
	end

	# 3. Rename each identifier and check compilation
	print "Renaming check... "
	STDOUT.flush
	
	idents.each_with_index do |i, n|
		percents = (n.to_f / idents.length * 100).to_i
		print "#{percents}%"
		STDOUT.flush

		out = %x[../rename #{src} #{i['Offset']} RenamedIdent123 > tmp.go]
		if not $?.success? then
			puts FAIL
			$stats.fail += 1
			puts out
			puts "─"*72
			system "../showcursor #{src} #{i['Offset']}"
			puts "─"*72
			return
		end
		if not check_source? "tmp.go" then
			puts FAIL
			$stats.fail += 1
			puts "─"*72
			system "../showcursor #{src} #{i['Offset']}"
			puts "─"*72
			system "cat tmp.go"
			puts "─"*72
			return
		end
		print "\r#{" "*72}\rRenaming check... "
	end
	puts PASS
	$stats.ok += 1
end

if ARGV.one?
	run_test ARGV[0]
else
	Dir["test.*"].sort.each do |t|
		run_test t
	end
end

print_stats
