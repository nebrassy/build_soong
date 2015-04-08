// Copyright 2015 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package java

// This file contains the module types for compiling Java for Android, and converts the properties
// into the flags and filenames necessary to pass to the compiler.  The final creation of the rules
// is handled in builder.go

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/blueprint"
	"github.com/google/blueprint/pathtools"

	"android/soong/common"
)

// TODO:
// Autogenerated files:
//  AIDL
//  Proto
//  Renderscript
// Post-jar passes:
//  Proguard
//  Emma
//  Jarjar
//  Dex
// Rmtypedefs
// Jack
// DroidDoc
// Findbugs

// javaBase contains the properties and members used by all java module types, and implements
// the blueprint.Module interface.
type javaBase struct {
	common.AndroidModuleBase
	module JavaModuleType

	properties struct {
		// srcs: list of source files used to compile the Java module.  May be .java, .logtags, .proto,
		// or .aidl files.
		Srcs []string `android:"arch_variant,arch_subtract"`

		// resource_dirs: list of directories containing resources
		Resource_dirs []string `android:"arch_variant"`

		// no_standard_libraries: don't build against the default libraries (core-libart, core-junit,
		// ext, and framework for device targets)
		No_standard_libraries bool

		// javacflags: list of module-specific flags that will be used for javac compiles
		Javacflags []string `android:"arch_variant"`

		// dxflags: list of module-specific flags that will be used for dex compiles
		Dxflags []string `android:"arch_variant"`

		// java_libs: list of of java libraries that will be in the classpath
		Java_libs []string `android:"arch_variant"`

		// java_static_libs: list of java libraries that will be compiled into the resulting jar
		Java_static_libs []string `android:"arch_variant"`

		// manifest: manifest file to be included in resulting jar
		Manifest string

		// sdk_version: if not blank, set to the version of the sdk to compile against
		Sdk_version string

		// Set for device java libraries, and for host versions of device java libraries
		// built for testing
		Dex bool `blueprint:"mutated"`

		// jarjar_rules: if not blank, run jarjar using the specified rules file
		Jarjar_rules string
	}

	// output file suitable for inserting into the classpath of another compile
	classpathFile string

	// jarSpecs suitable for inserting classes from a static library into another jar
	classJarSpecs []jarSpec

	// jarSpecs suitable for inserting resources from a static library into another jar
	resourceJarSpecs []jarSpec

	// installed file for binary dependency
	installFile string
}

type JavaModuleType interface {
	GenerateJavaBuildActions(ctx common.AndroidModuleContext)
}

type JavaDependency interface {
	ClasspathFile() string
	ClassJarSpecs() []jarSpec
	ResourceJarSpecs() []jarSpec
}

func NewJavaBase(base *javaBase, module JavaModuleType, hod common.HostOrDeviceSupported,
	props ...interface{}) (blueprint.Module, []interface{}) {

	base.module = module

	props = append(props, &base.properties)

	return common.InitAndroidArchModule(base, hod, common.MultilibCommon, props...)
}

func (j *javaBase) BootClasspath(ctx common.AndroidBaseContext) string {
	if ctx.Device() {
		if j.properties.Sdk_version == "" {
			return "core-libart"
		} else if j.properties.Sdk_version == "current" {
			// TODO: !TARGET_BUILD_APPS
			return "android_stubs_current"
		} else if j.properties.Sdk_version == "system_current" {
			return "android_system_stubs_current"
		} else {
			return "sdk_v" + j.properties.Sdk_version
		}
	} else {
		if j.properties.Dex {
			return "core-libart"
		} else {
			return ""
		}
	}
}

func (j *javaBase) AndroidDynamicDependencies(ctx common.AndroidDynamicDependerModuleContext) []string {
	var deps []string

	if !j.properties.No_standard_libraries {
		bootClasspath := j.BootClasspath(ctx)
		if bootClasspath != "" {
			deps = append(deps, bootClasspath)
		}
	}
	deps = append(deps, j.properties.Java_libs...)
	deps = append(deps, j.properties.Java_static_libs...)

	return deps
}

func (j *javaBase) collectDeps(ctx common.AndroidModuleContext) (classpath []string,
	bootClasspath string, classJarSpecs, resourceJarSpecs []jarSpec) {

	ctx.VisitDirectDeps(func(module blueprint.Module) {
		otherName := ctx.OtherModuleName(module)
		if javaDep, ok := module.(JavaDependency); ok {
			if inList(otherName, j.properties.Java_libs) {
				classpath = append(classpath, javaDep.ClasspathFile())
			} else if inList(otherName, j.properties.Java_static_libs) {
				classpath = append(classpath, javaDep.ClasspathFile())
				classJarSpecs = append(classJarSpecs, javaDep.ClassJarSpecs()...)
				resourceJarSpecs = append(resourceJarSpecs, javaDep.ResourceJarSpecs()...)
			} else if otherName == j.BootClasspath(ctx) {
				bootClasspath = javaDep.ClasspathFile()
			} else {
				panic(fmt.Errorf("unknown dependency %q for %q", otherName, ctx.ModuleName()))
			}
		} else {
			ctx.ModuleErrorf("unknown dependency module type for %q", otherName)
		}
	})

	return classpath, bootClasspath, classJarSpecs, resourceJarSpecs
}

func (j *javaBase) GenerateAndroidBuildActions(ctx common.AndroidModuleContext) {
	j.module.GenerateJavaBuildActions(ctx)
}

func (j *javaBase) GenerateJavaBuildActions(ctx common.AndroidModuleContext) {
	flags := javaBuilderFlags{
		javacFlags: strings.Join(j.properties.Javacflags, " "),
	}

	var javacDeps []string

	srcFiles := j.properties.Srcs
	srcFiles = pathtools.PrefixPaths(srcFiles, common.ModuleSrcDir(ctx))
	srcFiles = common.ExpandGlobs(ctx, srcFiles)

	classpath, bootClasspath, classJarSpecs, resourceJarSpecs := j.collectDeps(ctx)

	if bootClasspath != "" {
		flags.bootClasspath = "-bootclasspath " + bootClasspath
		javacDeps = append(javacDeps, bootClasspath)
	}

	if len(classpath) > 0 {
		flags.classpath = "-classpath " + strings.Join(classpath, ":")
		javacDeps = append(javacDeps, classpath...)
	}

	// Compile java sources into .class files
	classes := TransformJavaToClasses(ctx, srcFiles, flags, javacDeps)
	if ctx.Failed() {
		return
	}

	resourceJarSpecs = append(ResourceDirsToJarSpecs(ctx, j.properties.Resource_dirs), resourceJarSpecs...)
	classJarSpecs = append([]jarSpec{classes}, classJarSpecs...)

	manifest := j.properties.Manifest
	if manifest != "" {
		manifest = filepath.Join(common.ModuleSrcDir(ctx), manifest)
	}

	allJarSpecs := append([]jarSpec(nil), classJarSpecs...)
	allJarSpecs = append(allJarSpecs, resourceJarSpecs...)

	// Combine classes + resources into classes-full-debug.jar
	outputFile := TransformClassesToJar(ctx, allJarSpecs, manifest)
	if ctx.Failed() {
		return
	}

	j.classJarSpecs = classJarSpecs
	j.resourceJarSpecs = resourceJarSpecs

	if j.properties.Jarjar_rules != "" {
		jarjar_rules := filepath.Join(common.ModuleSrcDir(ctx), j.properties.Jarjar_rules)
		// Transform classes-full-debug.jar into classes-jarjar.jar
		outputFile = TransformJarJar(ctx, outputFile, jarjar_rules)
		if ctx.Failed() {
			return
		}
	}

	j.classpathFile = outputFile

	if j.properties.Dex {
		dxFlags := j.properties.Dxflags
		if false /* emma enabled */ {
			// If you instrument class files that have local variable debug information in
			// them emma does not correctly maintain the local variable table.
			// This will cause an error when you try to convert the class files for Android.
			// The workaround here is to build different dex file here based on emma switch
			// then later copy into classes.dex. When emma is on, dx is run with --no-locals
			// option to remove local variable information
			dxFlags = append(dxFlags, "--no-locals")
		}

		if ctx.AConfig().Getenv("NO_OPTIMIZE_DX") != "" {
			dxFlags = append(dxFlags, "--no-optimize")
		}

		if ctx.AConfig().Getenv("GENERATE_DEX_DEBUG") != "" {
			dxFlags = append(dxFlags,
				"--debug",
				"--verbose",
				"--dump-to="+filepath.Join(common.ModuleOutDir(ctx), "classes.lst"),
				"--dump-width=1000")
		}

		flags.dxFlags = strings.Join(dxFlags, " ")

		// Compile classes.jar into classes.dex
		dexFile := TransformClassesJarToDex(ctx, outputFile, flags)
		if ctx.Failed() {
			return
		}

		// Combine classes.dex + resources into javalib.jar
		outputFile = TransformDexToJavaLib(ctx, resourceJarSpecs, dexFile)
	}

	j.installFile = ctx.InstallFileName("framework", ctx.ModuleName()+".jar", outputFile)
}

var _ JavaDependency = (*JavaLibrary)(nil)

func (j *javaBase) ClasspathFile() string {
	return j.classpathFile
}

func (j *javaBase) ClassJarSpecs() []jarSpec {
	return j.classJarSpecs
}

func (j *javaBase) ResourceJarSpecs() []jarSpec {
	return j.resourceJarSpecs
}

//
// Java libraries (.jar file)
//

type JavaLibrary struct {
	javaBase
}

func JavaLibraryFactory() (blueprint.Module, []interface{}) {
	module := &JavaLibrary{}

	module.properties.Dex = true

	return NewJavaBase(&module.javaBase, module, common.HostAndDeviceSupported)
}

func JavaLibraryHostFactory() (blueprint.Module, []interface{}) {
	module := &JavaLibrary{}

	return NewJavaBase(&module.javaBase, module, common.HostSupported)
}

//
// Java Binaries (.jar file plus wrapper script)
//

type JavaBinary struct {
	JavaLibrary

	binaryProperties struct {
		// wrapper: installable script to execute the resulting jar
		Wrapper string
	}
}

func (j *JavaBinary) GenerateJavaBuildActions(ctx common.AndroidModuleContext) {
	j.JavaLibrary.GenerateJavaBuildActions(ctx)

	// Depend on the installed jar (j.installFile) so that the wrapper doesn't get executed by
	// another build rule before the jar has been installed.
	ctx.InstallFile("bin", filepath.Join(common.ModuleSrcDir(ctx), j.binaryProperties.Wrapper),
		j.installFile)
}

func JavaBinaryFactory() (blueprint.Module, []interface{}) {
	module := &JavaBinary{}

	module.properties.Dex = true

	return NewJavaBase(&module.javaBase, module, common.HostAndDeviceSupported, &module.binaryProperties)
}

func JavaBinaryHostFactory() (blueprint.Module, []interface{}) {
	module := &JavaBinary{}

	return NewJavaBase(&module.javaBase, module, common.HostSupported, &module.binaryProperties)
}

//
// Java prebuilts
//

type JavaPrebuilt struct {
	common.AndroidModuleBase

	properties struct {
		Srcs []string
	}

	classpathFile                   string
	classJarSpecs, resourceJarSpecs []jarSpec
}

func (j *JavaPrebuilt) GenerateAndroidBuildActions(ctx common.AndroidModuleContext) {
	if len(j.properties.Srcs) != 1 {
		ctx.ModuleErrorf("expected exactly one jar in srcs")
		return
	}
	prebuilt := filepath.Join(common.ModuleSrcDir(ctx), j.properties.Srcs[0])

	classJarSpec, resourceJarSpec := TransformPrebuiltJarToClasses(ctx, prebuilt)

	j.classpathFile = prebuilt
	j.classJarSpecs = []jarSpec{classJarSpec}
	j.resourceJarSpecs = []jarSpec{resourceJarSpec}

	ctx.InstallFileName("framework", ctx.ModuleName()+".jar", j.classpathFile)
}

var _ JavaDependency = (*JavaPrebuilt)(nil)

func (j *JavaPrebuilt) ClasspathFile() string {
	return j.classpathFile
}

func (j *JavaPrebuilt) ClassJarSpecs() []jarSpec {
	return j.classJarSpecs
}

func (j *JavaPrebuilt) ResourceJarSpecs() []jarSpec {
	return j.resourceJarSpecs
}

func JavaPrebuiltFactory() (blueprint.Module, []interface{}) {
	module := &JavaPrebuilt{}

	return common.InitAndroidArchModule(module, common.HostAndDeviceSupported,
		common.MultilibCommon, &module.properties)
}

func inList(s string, l []string) bool {
	for _, e := range l {
		if e == s {
			return true
		}
	}
	return false
}
