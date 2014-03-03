package hamlengine

import (
  "os"
  "io/ioutil"
  "log"
  "fmt"
  "strings"
  "path/filepath"
  "html/template"
  "github.com/golib/revel"
  "github.com/realistschuckle/gohaml"
)

func init() {
  revel.RegisterTemplateEnginer("haml", revel.TemplateEnginer(new(hamlTemplateEngine)))
}

type hamlTemplateEngine struct {
  revel.TemplateEngine
}

func (haml *hamlTemplateEngine) Refresh() *revel.Error {
  paths := haml.Paths()

  revel.TRACE.Printf("Refreshing templates from %s", paths)

  var compileError *revel.Error
  var templatePaths map[string]string

  // Set the template delimiters for the project if present, then split into left
  // and right delimiters around a space character
  var splitDelims []string
  if revel.TemplateDelims != "" {
    splitDelims = strings.Split(revel.TemplateDelims, " ")
    if len(splitDelims) != 2 {
      log.Fatalln("app.conf: Incorrect format for template.delimiters")
    }
  }

  var hamlScope = make(map[string]interface{})
  hamlScope["lang"] = "HAML"

  // Walk through the template engine's paths and build up a template set.
  var templateSet *template.Template = nil

  for _, basePath := range paths {
    // Walk only returns an error if the template engine is completely unusable
    // (namely, if one of the TemplateFuncs does not have an acceptable signature).
    funcErr := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
      if err != nil {
        revel.ERROR.Println("error walking templates:", err)
        return nil
      }

      // Walk into watchable directories
      if info.IsDir() {
        if !haml.WatchDir(info) {
          return filepath.SkipDir
        }
        return nil
      }

      // Only add watchable
      if !haml.WatchFile(info.Name()) {
        return nil
      }

      var fileStr string

      // addTemplate allows the same template to be added multiple
      // times with different template names.
      addTemplate := func(templateName string) (err error) {
        // Convert template names to use forward slashes, even on Windows.
        if os.PathSeparator == '\\' {
          templateName = strings.Replace(templateName, `\`, `/`, -1) // `
        }

        // If we already loaded a template of this name, skip it.
        if _, ok := templatePaths[templateName]; ok {
          return nil
        }
        templatePaths[templateName] = path

        // Load the file if we haven't already
        if fileStr == "" {
          fileBytes, err := ioutil.ReadFile(path)
          if err != nil {
            revel.ERROR.Println("Failed reading file:", path)
            return nil
          }

          hamlEngine, _ := gohaml.NewEngine(string(fileBytes))
          fileStr = hamlEngine.Render(hamlScope)
        }

        if templateSet == nil {
          // Create the template set.  This panics if any of the funcs do not
          // conform to expectations, so we wrap it in a func and handle those
          // panics by serving an error page.
          var funcError *revel.Error
          func() {
            defer func() {
              if err := recover(); err != nil {
                funcError = &revel.Error{
                  Title:       "Panic (Template engine)",
                  Description: fmt.Sprintln(err),
                }
              }
            }()
            templateSet = template.New(templateName).Funcs(revel.TemplateFuncs)
            // If alternate delimiters set for the project, change them for this set
            if splitDelims != nil && basePath == revel.ViewsPath {
              templateSet.Delims(splitDelims[0], splitDelims[1])
            } else {
              // Reset to default otherwise
              templateSet.Delims("", "")
            }
            _, err = templateSet.Parse(fileStr)
          }()

          if funcError != nil {
            return funcError
          }

        } else {
          if splitDelims != nil && basePath == revel.ViewsPath {
            templateSet.Delims(splitDelims[0], splitDelims[1])
          } else {
            templateSet.Delims("", "")
          }
          _, err = templateSet.New(templateName).Parse(fileStr)
        }
        return err
      }

      templateName := path[len(basePath)+1:]

      // Lower case the file name for case-insensitive matching
      lowerCaseTemplateName := strings.ToLower(templateName)

      err = addTemplate(templateName)
      err = addTemplate(lowerCaseTemplateName)

      // Store / report the first error encountered.
      if err != nil && compileError == nil {
        _, line, description := revel.ParseTemplateError(err)
        compileError = &revel.Error{
          Title:       "Template Compilation Error",
          Path:        templateName,
          Description: description,
          Line:        line,
          SourceLines: strings.Split(fileStr, "\n"),
        }

        revel.ERROR.Printf("Template compilation error (In %s around line %d):\n%s", templateName, line, description)
      }
      return nil
    })

    // If there was an error with the Funcs, set it and return immediately.
    if funcErr != nil {
      compileError = funcErr.(*revel.Error)
      haml.SetCompileError(compileError)

      return compileError
    }
  }

  // Note: compileError may or may not be set.
  haml.SetTemplateSet(templateSet)

  return compileError
}
