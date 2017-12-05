extern crate codegen;

use codegen::Scope;

#[test]
fn empty_scope() {
    let scope = Scope::new();

    assert_eq!(scope.to_string(), "");
}

#[test]
fn single_struct() {
    let mut scope = Scope::new();

    scope.structure("Foo")
        .field("one", "usize")
        .field("two", "String");

    let expect = r#"
struct Foo {
    one: usize,
    two: String,
}"#;

    assert_eq!(scope.to_string(), &expect[1..]);
}

#[test]
fn two_structs() {
    let mut scope = Scope::new();

    scope.structure("Foo")
        .field("one", "usize")
        .field("two", "String");

    scope.structure("Bar")
        .field("hello", "World");

    let expect = r#"
struct Foo {
    one: usize,
    two: String,
}

struct Bar {
    hello: World,
}"#;

    assert_eq!(scope.to_string(), &expect[1..]);
}

#[test]
fn struct_with_derive() {
    let mut scope = Scope::new();

    scope.structure("Foo")
        .derive("Debug").derive("Clone")
        .field("one", "usize")
        .field("two", "String");

    let expect = r#"
#[derive(Debug, Clone)]
struct Foo {
    one: usize,
    two: String,
}"#;

    assert_eq!(scope.to_string(), &expect[1..]);
}

#[test]
fn struct_with_generics_1() {
    let mut scope = Scope::new();

    scope.structure("Foo")
        .generic("T")
        .generic("U")
        .field("one", "T")
        .field("two", "U");

    let expect = r#"
struct Foo<T, U> {
    one: T,
    two: U,
}"#;

    assert_eq!(scope.to_string(), &expect[1..]);
}

#[test]
fn struct_with_generics_2() {
    let mut scope = Scope::new();

    scope.structure("Foo")
        .generic("T, U")
        .field("one", "T")
        .field("two", "U");

    let expect = r#"
struct Foo<T, U> {
    one: T,
    two: U,
}"#;

    assert_eq!(scope.to_string(), &expect[1..]);
}

#[test]
fn struct_with_generics_3() {
    let mut scope = Scope::new();

    scope.structure("Foo")
        .generic("T: Win, U")
        .field("one", "T")
        .field("two", "U");

    let expect = r#"
struct Foo<T: Win, U> {
    one: T,
    two: U,
}"#;

    assert_eq!(scope.to_string(), &expect[1..]);
}

#[test]
fn struct_where_clause_1() {
    let mut scope = Scope::new();

    scope.structure("Foo")
        .generic("T")
        .bound("T", "Foo")
        .field("one", "T");

    let expect = r#"
struct Foo<T>
where T: Foo,
{
    one: T,
}"#;

    assert_eq!(scope.to_string(), &expect[1..]);
}

#[test]
fn struct_where_clause_2() {
    let mut scope = Scope::new();

    scope.structure("Foo")
        .generic("T, U")
        .bound("T", "Foo")
        .bound("U", "Baz")
        .field("one", "T")
        .field("two", "U");

    let expect = r#"
struct Foo<T, U>
where T: Foo,
      U: Baz,
{
    one: T,
    two: U,
}"#;

    assert_eq!(scope.to_string(), &expect[1..]);
}

#[test]
fn struct_doc() {
    let mut scope = Scope::new();

    scope.structure("Foo")
        .doc("Hello, this is a doc string\n\
              that continues on another line.")
        .field("one", "T");

    let expect = r#"
/// Hello, this is a doc string
/// that continues on another line.
struct Foo {
    one: T,
}"#;

    assert_eq!(scope.to_string(), &expect[1..]);
}

#[test]
fn struct_in_mod() {
    let mut scope = Scope::new();

    {
        let module = scope.module("foo");
        module.structure("Foo")
            .doc("Hello some docs")
            .derive("Debug")
            .generic("T, U")
            .bound("T", "SomeBound")
            .bound("U", "SomeOtherBound")
            .field("one", "T")
            .field("two", "U")
            ;
    }

    let expect = r#"
mod foo {
    /// Hello some docs
    #[derive(Debug)]
    struct Foo<T, U>
    where T: SomeBound,
          U: SomeOtherBound,
    {
        one: T,
        two: U,
    }
}"#;

    assert_eq!(scope.to_string(), &expect[1..]);
}

#[test]
fn struct_mod_import() {
    let mut scope = Scope::new();
    scope.module("foo")
        .import("bar", "Bar")
        .structure("Foo")
        .field("bar", "Bar")
        ;

    let expect = r#"
mod foo {
    use bar::Bar;

    struct Foo {
        bar: Bar,
    }
}"#;

    assert_eq!(scope.to_string(), &expect[1..]);
}
