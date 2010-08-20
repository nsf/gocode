// This package implements a parser for configuration files.
// This allows easy reading and writing of structured configuration files.
//
// Given a sample configuration file:
//
//	[default]
//	host=www.example.com
//	protocol=http://
//	base-url=%(protocol)s%(host)s
//
//	[service-1]
//	url=%(base-url)s/some/path
//	delegation : on
//	maxclients=200 # do not set this higher
//	comments=This is a multi-line
//		entry	; And this is a comment
//
// To read this configuration file, do:
//
//	c, err := configfile.ReadConfigFile("config.cfg");
//	c.GetString("service-1", "url"); // result is string :http://www.example.com/some/path"
//	c.GetInt("service-1", "maxclients"); // result is int 200
//	c.GetBool("service-1", "delegation"); // result is bool true
//	c.GetString("service-1", "comments"); // result is string "This is a multi-line\nentry"
//
// Note the support for unfolding variables (such as %(base-url)s), which are read from the special
// (reserved) section name [default].
//
// A new configuration file can also be created with:
//
//	c := configfile.NewConfigFile();
//	c.AddSection("section");
//	c.AddOption("section", "option", "value");
//	c.WriteConfigFile("config.cfg", 0644, "A header for this file"); // use 0644 as file permission
//
// This results in the file:
//
//	# A header for this file
//	[section]
//	option=value
//
// Note that sections and options are case-insensitive (values are case-sensitive)
// and are converted to lowercase when saved to a file.
//
// The functionality and workflow is loosely based on the configparser.py package
// of the Python Standard Library.
package configfile


import (
	"bufio";
	"os";
	"regexp";
	"strconv";
	"strings";
)


// ConfigFile is the representation of configuration settings.
// The public interface is entirely through methods.
type ConfigFile struct {
	data map[string]map[string]string;	// Maps sections to options to values.
}


var (
	DefaultSection	= "default";	// Default section name (must be lower-case).
	DepthValues	= 200;		// Maximum allowed depth when recursively substituing variable names.

	// Strings accepted as bool.
	BoolStrings	= map[string]bool{
		"t": true,
		"true": true,
		"y": true,
		"yes": true,
		"on": true,
		"1": true,
		"f": false,
		"false": false,
		"n": false,
		"no": false,
		"off": false,
		"0": false,
	};

	varRegExp	= regexp.MustCompile(`%\(([a-zA-Z0-9_.\-]+)\)s`);
)


// AddSection adds a new section to the configuration.
// It returns true if the new section was inserted, and false if the section already existed.
func (c *ConfigFile) AddSection(section string) bool {
	section = strings.ToLower(section);

	if _, ok := c.data[section]; ok {
		return false
	}
	c.data[section] = make(map[string]string);

	return true;
}


// RemoveSection removes a section from the configuration.
// It returns true if the section was removed, and false if section did not exist.
func (c *ConfigFile) RemoveSection(section string) bool {
	section = strings.ToLower(section);

	switch _, ok := c.data[section]; {
	case !ok:
		return false
	case section == DefaultSection:
		return false	// default section cannot be removed
	default:
		for o, _ := range c.data[section] {
			c.data[section][o] = "", false
		}
		c.data[section] = nil, false;
	}

	return true;
}


// AddOption adds a new option and value to the configuration.
// It returns true if the option and value were inserted, and false if the value was overwritten.
// If the section does not exist in advance, it is created.
func (c *ConfigFile) AddOption(section string, option string, value string) bool {
	c.AddSection(section);	// make sure section exists

	section = strings.ToLower(section);
	option = strings.ToLower(option);

	_, ok := c.data[section][option];
	c.data[section][option] = value;

	return !ok;
}


// RemoveOption removes a option and value from the configuration.
// It returns true if the option and value were removed, and false otherwise,
// including if the section did not exist.
func (c *ConfigFile) RemoveOption(section string, option string) bool {
	section = strings.ToLower(section);
	option = strings.ToLower(option);

	if _, ok := c.data[section]; !ok {
		return false
	}

	_, ok := c.data[section][option];
	c.data[section][option] = "", false;

	return ok;
}


// NewConfigFile creates an empty configuration representation.
// This representation can be filled with AddSection and AddOption and then
// saved to a file using WriteConfigFile.
func NewConfigFile() *ConfigFile {
	c := new(ConfigFile);
	c.data = make(map[string]map[string]string);

	c.AddSection(DefaultSection);	// default section always exists

	return c;
}


func stripComments(l string) string {
	// comments are preceded by space or TAB
	for _, c := range []string{" ;", "\t;", " #", "\t#"} {
		if i := strings.Index(l, c); i != -1 {
			l = l[0:i]
		}
	}
	return l;
}


func firstIndex(s string, delim []byte) int {
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(delim); j++ {
			if s[i] == delim[j] {
				return i
			}
		}
	}
	return -1;
}


func (c *ConfigFile) read(buf *bufio.Reader) (err os.Error) {
	var section, option string;
	for {
		l, err := buf.ReadString('\n');	// parse line-by-line
		if err == os.EOF {
			break
		} else if err != nil {
			return err
		}

		l = strings.TrimSpace(l);
		// switch written for readability (not performance)
		switch {
		case len(l) == 0:	// empty line
			continue

		case l[0] == '#':	// comment
			continue

		case l[0] == ';':	// comment
			continue

		case len(l) >= 3 && strings.ToLower(l[0:3]) == "rem":	// comment (for windows users)
			continue

		case l[0] == '[' && l[len(l)-1] == ']':	// new section
			option = "";	// reset multi-line value
			section = strings.TrimSpace(l[1 : len(l)-1]);
			c.AddSection(section);

		case section == "":	// not new section and no section defined so far
			return os.NewError("section not found: must start with section")

		default:	// other alternatives
			i := firstIndex(l, []byte{'=', ':'});
			switch {
			case i > 0:	// option and value
				i := firstIndex(l, []byte{'=', ':'});
				option = strings.TrimSpace(l[0:i]);
				value := strings.TrimSpace(stripComments(l[i+1:]));
				c.AddOption(section, option, value);

			case section != "" && option != "":	// continuation of multi-line value
				prev, _ := c.GetRawString(section, option);
				value := strings.TrimSpace(stripComments(l));
				c.AddOption(section, option, prev+"\n"+value);

			default:
				return os.NewError("could not parse line: " + l)
			}
		}
	}
	return nil;
}


// ReadConfigFile reads a file and returns a new configuration representation.
// This representation can be queried with GetString, etc.
func ReadConfigFile(fname string) (c *ConfigFile, err os.Error) {
	var file *os.File;

	if file, err = os.Open(fname, os.O_RDONLY, 0); err != nil {
		return nil, err
	}

	c = NewConfigFile();
	if err = c.read(bufio.NewReader(file)); err != nil {
		return nil, err
	}

	if err = file.Close(); err != nil {
		return nil, err
	}

	return c, nil;
}


func (c *ConfigFile) write(buf *bufio.Writer, header string) (err os.Error) {
	if header != "" {
		if _, err = buf.WriteString("# " + header + "\n"); err != nil {
			return err
		}
	}

	for section, sectionmap := range c.data {
		if section == DefaultSection && len(sectionmap) == 0 {
			continue	// skip default section if empty
		}
		if _, err = buf.WriteString("[" + section + "]\n"); err != nil {
			return err
		}
		for option, value := range sectionmap {
			if _, err = buf.WriteString(option + "=" + value + "\n"); err != nil {
				return err
			}
		}
		if _, err = buf.WriteString("\n"); err != nil {
			return err
		}
	}

	return nil;
}


// WriteConfigFile saves the configuration representation to a file.
// The desired file permissions must be passed as in os.Open.
// The header is a string that is saved as a comment in the first line of the file.
func (c *ConfigFile) WriteConfigFile(fname string, perm uint32, header string) (err os.Error) {
	var file *os.File;

	if file, err = os.Open(fname, os.O_WRONLY|os.O_CREAT|os.O_TRUNC, perm); err != nil {
		return err
	}

	buf := bufio.NewWriter(file);
	if err = c.write(buf, header); err != nil {
		return err
	}
	buf.Flush();

	return file.Close();
}


// GetSections returns the list of sections in the configuration.
// (The default section always exists.)
func (c *ConfigFile) GetSections() (sections []string) {
	sections = make([]string, len(c.data));

	i := 0;
	for s, _ := range c.data {
		sections[i] = s;
		i++;
	}

	return sections;
}


// HasSection checks if the configuration has the given section.
// (The default section always exists.)
func (c *ConfigFile) HasSection(section string) bool {
	_, ok := c.data[strings.ToLower(section)];

	return ok;
}


// GetOptions returns the list of options available in the given section.
// It returns an error if the section does not exist and an empty list if the section is empty.
// Options within the default section are also included.
func (c *ConfigFile) GetOptions(section string) (options []string, err os.Error) {
	section = strings.ToLower(section);

	if _, ok := c.data[section]; !ok {
		return nil, os.NewError("section not found")
	}

	options = make([]string, len(c.data[DefaultSection])+len(c.data[section]));
	i := 0;
	for s, _ := range c.data[DefaultSection] {
		options[i] = s;
		i++;
	}
	for s, _ := range c.data[section] {
		options[i] = s;
		i++;
	}

	return options, nil;
}


// HasOption checks if the configuration has the given option in the section.
// It returns false if either the option or section do not exist.
func (c *ConfigFile) HasOption(section string, option string) bool {
	section = strings.ToLower(section);
	option = strings.ToLower(option);

	if _, ok := c.data[section]; !ok {
		return false
	}

	_, okd := c.data[DefaultSection][option];
	_, oknd := c.data[section][option];

	return okd || oknd;
}


// GetRawString gets the (raw) string value for the given option in the section.
// The raw string value is not subjected to unfolding, which was illustrated in the beginning of this documentation.
// It returns an error if either the section or the option do not exist.
func (c *ConfigFile) GetRawString(section string, option string) (value string, err os.Error) {
	section = strings.ToLower(section);
	option = strings.ToLower(option);

	if _, ok := c.data[section]; ok {
		if value, ok = c.data[section][option]; ok {
			return value, nil
		}
		return "", os.NewError("option not found");
	}
	return "", os.NewError("section not found");
}


// GetString gets the string value for the given option in the section.
// If the value needs to be unfolded (see e.g. %(host)s example in the beginning of this documentation),
// then GetString does this unfolding automatically, up to DepthValues number of iterations.
// It returns an error if either the section or the option do not exist, or the unfolding cycled.
func (c *ConfigFile) GetString(section string, option string) (value string, err os.Error) {
	value, err = c.GetRawString(section, option);
	if err != nil {
		return "", err
	}

	section = strings.ToLower(section);

	var i int;

	for i = 0; i < DepthValues; i++ {	// keep a sane depth
		vr := varRegExp.FindStringSubmatchIndex(value);
		if len(vr) == 0 {
			break
		}

		noption := value[vr[2]:vr[3]];
		noption = strings.ToLower(noption);

		nvalue, _ := c.data[DefaultSection][noption];	// search variable in default section
		if _, ok := c.data[section][noption]; ok {
			nvalue = c.data[section][noption]
		}
		if nvalue == "" {
			return "", os.NewError("option not found: " + noption)
		}

		// substitute by new value and take off leading '%(' and trailing ')s'
		value = value[0:vr[2]-2] + nvalue + value[vr[3]+2:];
	}

	if i == DepthValues {
		return "", os.NewError("possible cycle while unfolding variables: max depth of " + strconv.Itoa(DepthValues) + " reached")
	}

	return value, nil;
}


// GetInt has the same behaviour as GetString but converts the response to int.
func (c *ConfigFile) GetInt(section string, option string) (value int, err os.Error) {
	sv, err := c.GetString(section, option);
	if err == nil {
		value, err = strconv.Atoi(sv)
	}

	return value, err;
}


// GetFloat has the same behaviour as GetString but converts the response to float.
func (c *ConfigFile) GetFloat(section string, option string) (value float, err os.Error) {
	sv, err := c.GetString(section, option);
	if err == nil {
		value, err = strconv.Atof(sv)
	}

	return value, err;
}


// GetBool has the same behaviour as GetString but converts the response to bool.
// See constant BoolStrings for string values converted to bool.
func (c *ConfigFile) GetBool(section string, option string) (value bool, err os.Error) {
	sv, err := c.GetString(section, option);
	if err != nil {
		return false, err
	}

	value, ok := BoolStrings[strings.ToLower(sv)];
	if !ok {
		return false, os.NewError("could not parse bool value: " + sv)
	}

	return value, nil;
}
