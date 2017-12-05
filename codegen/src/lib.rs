extern crate ordermap;

use ordermap::OrderMap;
use std::fmt::{self, Write};

/// Defines a scope.
///
/// A scope contains modules, types, etc...
#[derive(Debug, Clone)]
pub struct Scope {
    /// Scope documentation
    docs: Option<Docs>,

    /// Imports
    imports: OrderMap<String, OrderMap<String, Import>>,

    /// Contents of the documentation,
    items: Vec<Item>,
}

#[derive(Debug, Clone)]
enum Item {
    Module(Module),
    Struct(Struct),
    Enum(Enum),
    Impl(Impl),
}

/// Defines a module
#[derive(Debug, Clone)]
pub struct Module {
    /// Module name
    name: String,

    /// Visibility
    vis: Option<String>,

    /// Module documentation
    docs: Option<Docs>,

    /// Contents of the module
    scope: Scope,
}

/// Defines an enumeration
#[derive(Debug, Clone)]
pub struct Enum {
    type_def: TypeDef,
    variants: Vec<Variant>,
}

/// Defines a struct
#[derive(Debug, Clone)]
pub struct Struct {
    type_def: TypeDef,

    /// Struct fields
    fields: Fields,
}

#[derive(Debug, Clone)]
pub struct Type {
    name: String,
    generics: Vec<Type>,
}

/// A type definition
#[derive(Debug, Clone)]
struct TypeDef {
    ty: Type,
    vis: Option<String>,
    docs: Option<Docs>,
    derive: Vec<String>,
    bounds: Vec<Field>,
}

/// An enum variant
#[derive(Debug, Clone)]
pub struct Variant {
    name: String,
    fields: Fields,
}

#[derive(Debug, Clone)]
enum Fields {
    Empty,
    Tuple(Vec<Type>),
    Named(Vec<Field>),
}

/// Defines a struct field
#[derive(Debug, Clone)]
struct Field {
    /// Field name
    name: String,

    /// Field type
    ty: Type,
}

#[derive(Debug, Clone)]
pub struct Impl {
    /// The struct being implemented
    target: Type,

    /// Impl level generics
    generics: Vec<String>,

    /// If implementing a trait
    impl_trait: Option<String>,

    /// Associated types
    assoc_tys: Vec<Field>,

    /// Bounds
    bounds: Vec<Field>,

    fns: Vec<Function>,
}

/// Import
#[derive(Debug, Clone)]
pub struct Import {
    line: String,
    vis: Option<String>,
}

/// A function definition
#[derive(Debug, Clone)]
pub struct Function {
    /// Name of the function
    name: String,

    /// Function documentation
    docs: Option<Docs>,

    /// Function visibility
    vis: Option<String>,

    /// Function generics
    generics: Vec<String>,

    /// If the function takes `&self` or `&mut self`
    arg_self: Option<String>,

    /// Function arguments
    args: Vec<Field>,

    /// Return type
    ret: Option<Type>,

    /// Where bounds
    bounds: Vec<Field>,

    /// Body contents
    body: Vec<Body>,
}

/// A block of code
#[derive(Debug, Clone)]
pub struct Block {
    before: Option<String>,
    after: Option<String>,
    body: Vec<Body>,
}

#[derive(Debug, Clone)]
enum Body {
    String(String),
    Block(Block),
}

#[derive(Debug, Clone)]
struct Docs {
    docs: String,
}

/// Formatting configuration
#[derive(Debug)]
pub struct Formatter<'a> {
    /// Write destination
    dst: &'a mut String,

    /// Number of spaces to start a new line with
    spaces: usize,

    /// Number of spaces per indentiation
    indent: usize,
}

const DEFAULT_INDENT: usize = 4;

// ===== impl Scope =====

impl Scope {
    /// Returns a new scope
    pub fn new() -> Self {
        Scope {
            docs: None,
            imports: OrderMap::new(),
            items: vec![],
        }
    }

    /// Push an `use` line
    pub fn import(&mut self, path: &str, ty: &str) -> &mut Import {
        self.imports.entry(path.to_string())
            .or_insert(OrderMap::new())
            .entry(ty.to_string())
            .or_insert_with(|| Import::new(path, ty))
    }

    /// Pushes a new module definition, returning a mutable reference to the
    /// definition.
    pub fn module(&mut self, name: &str) -> &mut Module {
        self.push_module(Module::new(name));

        match *self.items.last_mut().unwrap() {
            Item::Module(ref mut v) => v,
            _ => unreachable!(),
        }
    }

    /// Push a module definition
    pub fn push_module(&mut self, module: Module) -> &mut Self {
        self.items.push(Item::Module(module));
        self
    }

    /// Push a new struct definition, returning a mutable reference to the
    /// definition.
    pub fn structure(&mut self, name: &str) -> &mut Struct {
        self.push_structure(Struct::new(name));

        match *self.items.last_mut().unwrap() {
            Item::Struct(ref mut v) => v,
            _ => unreachable!(),
        }
    }

    /// Push a structure definition
    pub fn push_structure(&mut self, structure: Struct) -> &mut Self {
        self.items.push(Item::Struct(structure));
        self
    }

    /// Push a new struct definition, returning a mutable reference to the
    /// definition.
    pub fn enumeration(&mut self, name: &str) -> &mut Enum {
        self.push_enumeration(Enum::new(name));

        match *self.items.last_mut().unwrap() {
            Item::Enum(ref mut v) => v,
            _ => unreachable!(),
        }
    }

    /// Push a structure definition
    pub fn push_enumeration(&mut self, enumeration: Enum) -> &mut Self {
        self.items.push(Item::Enum(enumeration));
        self
    }

    pub fn imp(&mut self, target: &str) -> &mut Impl {
        self.push_imp(Impl::new(target));

        match *self.items.last_mut().unwrap() {
            Item::Impl(ref mut v) => v,
            _ => unreachable!(),
        }
    }

    pub fn push_imp(&mut self, imp: Impl) -> &mut Self {
        self.items.push(Item::Impl(imp));
        self
    }

    /// Return a string representation of the scope.
    pub fn to_string(&self) -> String {
        let mut ret = String::new();

        self.fmt(&mut Formatter::new(&mut ret)).unwrap();

        // Remove the trailing newline
        if ret.as_bytes().last() == Some(&b'\n') {
            ret.pop();
        }

        ret
    }

    pub fn fmt(&self, fmt: &mut Formatter) -> fmt::Result {
        self.fmt_imports(fmt)?;

        if !self.imports.is_empty() {
            write!(fmt, "\n")?;
        }

        for (i, item) in self.items.iter().enumerate() {
            if i != 0 {
                write!(fmt, "\n")?;
            }

            match *item {
                Item::Module(ref v) => v.fmt(fmt)?,
                Item::Struct(ref v) => v.fmt(fmt)?,
                Item::Enum(ref v) => v.fmt(fmt)?,
                Item::Impl(ref v) => v.fmt(fmt)?,
            }
        }

        Ok(())
    }

    fn fmt_imports(&self, fmt: &mut Formatter) -> fmt::Result {
        // First, collect all visibilities
        let mut visibilities = vec![];

        for (_, imports) in &self.imports {
            for (_, import) in imports {
                if !visibilities.contains(&import.vis) {
                    visibilities.push(import.vis.clone());
                }
            }
        }

        let mut tys = vec![];

        // Loop over all visibilities and format the associated imports
        for vis in &visibilities {
            for (path, imports) in &self.imports {
                tys.clear();

                for (ty, import) in imports {
                    if *vis == import.vis {
                        tys.push(ty);
                    }
                }

                if !tys.is_empty() {
                    if let Some(ref vis) = *vis {
                        write!(fmt, "{} ", vis)?;
                    }

                    write!(fmt, "use {}::", path)?;

                    if tys.len() > 1 {
                        write!(fmt, "{{")?;

                        for (i, ty) in tys.iter().enumerate() {
                            if i != 0 { write!(fmt, ", ")?; }
                            write!(fmt, "{}", ty)?;
                        }

                        write!(fmt, "}};\n")?;
                    } else if tys.len() == 1 {
                        write!(fmt, "{};\n", tys[0])?;
                    }
                }
            }
        }

        Ok(())
    }
}

// ===== impl Module =====

impl Module {
    /// Return a new, blank module
    pub fn new(name: &str) -> Self {
        Module {
            name: name.to_string(),
            vis: None,
            docs: None,
            scope: Scope::new(),
        }
    }

    pub fn vis(&mut self, vis: &str) -> &mut Self {
        self.vis = Some(vis.to_string());
        self
    }

    /// Push an `use` line
    pub fn import(&mut self, path: &str, ty: &str) -> &mut Self {
        self.scope.import(path, ty);
        self
    }

    /// Pushes a new module definition, returning a mutable reference to the
    /// definition.
    pub fn module(&mut self, name: &str) -> &mut Module {
        self.scope.module(name)
    }

    /// Push a module definition
    pub fn push_module(&mut self, module: Module) -> &mut Self {
        self.scope.push_module(module);
        self
    }

    /// Push a new struct definition, returning a mutable reference to the
    /// definition.
    pub fn structure(&mut self, name: &str) -> &mut Struct {
        self.scope.structure(name)
    }

    /// Push a structure definition
    pub fn push_structure(&mut self, structure: Struct) -> &mut Self {
        self.scope.push_structure(structure);
        self
    }

    /// Push a new struct definition, returning a mutable reference to the
    /// definition.
    pub fn enumeration(&mut self, name: &str) -> &mut Enum {
        self.scope.enumeration(name)
    }

    /// Push a structure definition
    pub fn push_enumeration(&mut self, enumeration: Enum) -> &mut Self {
        self.scope.push_enumeration(enumeration);
        self
    }

    pub fn imp(&mut self, target: &str) -> &mut Impl {
        self.scope.imp(target)
    }

    pub fn push_imp(&mut self, imp: Impl) -> &mut Self {
        self.scope.push_imp(imp);
        self
    }

    pub fn fmt(&self, fmt: &mut Formatter) -> fmt::Result {
        if let Some(ref vis) = self.vis {
            write!(fmt, "{} ", vis)?;
        }

        write!(fmt, "mod {}", self.name)?;
        fmt.block(|fmt| {
            self.scope.fmt(fmt)
        })
    }
}

// ===== impl Struct =====

impl Struct {
    /// Return a structure definition with the provided name
    pub fn new(name: &str) -> Self {
        Struct {
            type_def: TypeDef::new(name),
            fields: Fields::Empty,
        }
    }

    /// Returns a reference to the type
    pub fn ty(&self) -> &Type {
        &self.type_def.ty
    }

    pub fn vis(&mut self, vis: &str) -> &mut Self {
        self.type_def.vis(vis);
        self
    }

    pub fn generic(&mut self, name: &str) -> &mut Self {
        self.type_def.ty.generic(name);
        self
    }

    pub fn bound<T>(&mut self, name: &str, ty: T) -> &mut Self
    where T: Into<Type>,
    {
        self.type_def.bound(name, ty);
        self
    }

    pub fn doc(&mut self, docs: &str) -> &mut Self {
        self.type_def.doc(docs);
        self
    }

    pub fn derive(&mut self, name: &str) -> &mut Self {
        self.type_def.derive(name);
        self
    }

    pub fn field<T>(&mut self, name: &str, ty: T) -> &mut Self
    where T: Into<Type>,
    {
        self.fields.named(name, ty);
        self
    }

    pub fn fmt(&self, fmt: &mut Formatter) -> fmt::Result {
        self.type_def.fmt_head("struct", fmt)?;
        self.fields.fmt(fmt)?;

        Ok(())
    }
}

// ===== impl Enum =====

impl Enum {
    /// Return a structure definition with the provided name
    pub fn new(name: &str) -> Self {
        Enum {
            type_def: TypeDef::new(name),
            variants: vec![],
        }
    }

    /// Returns a reference to the type
    pub fn ty(&self) -> &Type {
        &self.type_def.ty
    }

    pub fn vis(&mut self, vis: &str) -> &mut Self {
        self.type_def.vis(vis);
        self
    }

    pub fn generic(&mut self, name: &str) -> &mut Self {
        self.type_def.ty.generic(name);
        self
    }

    pub fn bound<T>(&mut self, name: &str, ty: T) -> &mut Self
    where T: Into<Type>,
    {
        self.type_def.bound(name, ty);
        self
    }

    pub fn doc(&mut self, docs: &str) -> &mut Self {
        self.type_def.doc(docs);
        self
    }

    pub fn derive(&mut self, name: &str) -> &mut Self {
        self.type_def.derive(name);
        self
    }

    pub fn variant(&mut self, name: &str) -> &mut Variant {
        self.push_variant(Variant::new(name));
        self.variants.last_mut().unwrap()
    }

    pub fn push_variant(&mut self, variant: Variant) -> &mut Self {
        self.variants.push(variant);
        self
    }

    pub fn fmt(&self, fmt: &mut Formatter) -> fmt::Result {
        self.type_def.fmt_head("enum", fmt)?;

        fmt.block(|fmt| {
            for variant in &self.variants {
                variant.fmt(fmt)?;
            }

            Ok(())
        })
    }
}

// ===== impl Variant =====

impl Variant {
    pub fn new(name: &str) -> Self {
        Variant {
            name: name.to_string(),
            fields: Fields::Empty,
        }
    }

    pub fn named<T>(&mut self, name: &str, ty: T) -> &mut Self
    where T: Into<Type>,
    {
        self.fields.named(name, ty);
        self
    }

    pub fn tuple(&mut self, ty: &str) -> &mut Self {
        self.fields.tuple(ty);
        self
    }

    pub fn fmt(&self, fmt: &mut Formatter) -> fmt::Result {
        write!(fmt, "{}", self.name)?;
        self.fields.fmt(fmt)?;
        write!(fmt, ",\n")?;

        Ok(())
    }
}

// ===== impl Type =====

impl Type {
    pub fn new(name: &str) -> Self {
        Type {
            name: name.to_string(),
            generics: vec![],
        }
    }

    pub fn generic<T>(&mut self, ty: T) -> &mut Self
    where T: Into<Type>,
    {
        // Make sure that the name doesn't already include generics
        assert!(!self.name.contains("<"), "type name already includes generics");

        self.generics.push(ty.into());
        self
    }

    /// Rewrite the `Type` with the provided path
    pub fn path(&self, path: &str) -> Type {
        // TODO: This isn't really correct
        assert!(!self.name.contains("::"));

        let mut name = path.to_string();
        name.push_str("::");
        name.push_str(&self.name);

        Type {
            name,
            generics: self.generics.clone(),
        }
    }

    pub fn fmt(&self, fmt: &mut Formatter) -> fmt::Result {
        write!(fmt, "{}", self.name)?;
        Type::fmt_slice(&self.generics, fmt)
    }

    fn fmt_slice(generics: &[Type], fmt: &mut Formatter) -> fmt::Result {
        if !generics.is_empty() {
            write!(fmt, "<")?;

            for (i, ty) in generics.iter().enumerate() {
                if i != 0 { write!(fmt, ", ")? }
                ty.fmt(fmt)?;
            }

            write!(fmt, ">")?;
        }

        Ok(())
    }
}

impl<'a> From<&'a str> for Type {
    fn from(src: &'a str) -> Self {
        Type::new(src)
    }
}

impl From<String> for Type {
    fn from(src: String) -> Self {
        Type {
            name: src,
            generics: vec![],
        }
    }
}

impl<'a> From<&'a String> for Type {
    fn from(src: &'a String) -> Self {
        Type::new(src)
    }
}

impl<'a> From<&'a Type> for Type {
    fn from(src: &'a Type) -> Self {
        src.clone()
    }
}

// ===== impl TypeDef =====

impl TypeDef {
    /// Return a structure definition with the provided name
    pub fn new(name: &str) -> Self {
        TypeDef {
            ty: Type::new(name),
            vis: None,
            docs: None,
            derive: vec![],
            bounds: vec![],
        }
    }

    fn vis(&mut self, vis: &str) {
        self.vis = Some(vis.to_string());
    }

    fn bound<T>(&mut self, name: &str, ty: T)
    where T: Into<Type>,
    {
        self.bounds.push(Field {
            name: name.to_string(),
            ty: ty.into(),
        });
    }

    fn doc(&mut self, docs: &str) {
        self.docs = Some(Docs::new(docs));
    }

    fn derive(&mut self, name: &str) {
        self.derive.push(name.to_string());
    }

    fn fmt_head(&self, keyword: &str, fmt: &mut Formatter) -> fmt::Result {
        if let Some(ref docs) = self.docs {
            docs.fmt(fmt)?;
        }

        self.fmt_derive(fmt)?;

        if let Some(ref vis) = self.vis {
            write!(fmt, "{} ", vis)?;
        }

        write!(fmt, "{} ", keyword)?;
        self.ty.fmt(fmt)?;

        fmt_bounds(&self.bounds, fmt)?;

        Ok(())
    }

    fn fmt_derive(&self, fmt: &mut Formatter) -> fmt::Result {
        if !self.derive.is_empty() {
            write!(fmt, "#[derive(")?;

            for (i, name) in self.derive.iter().enumerate() {
                if i != 0 { write!(fmt, ", ")? }
                write!(fmt, "{}", name)?;
            }

            write!(fmt, ")]\n")?;
        }

        Ok(())
    }
}

fn fmt_generics(generics: &[String], fmt: &mut Formatter) -> fmt::Result {
    if !generics.is_empty() {
        write!(fmt, "<")?;

        for (i, ty) in generics.iter().enumerate() {
            if i != 0 { write!(fmt, ", ")? }
            write!(fmt, "{}", ty)?;
        }

        write!(fmt, ">")?;
    }

    Ok(())
}

fn fmt_bounds(bounds: &[Field], fmt: &mut Formatter) -> fmt::Result {
    if !bounds.is_empty() {
        write!(fmt, "\n")?;

        // Write first bound
        write!(fmt, "where {}: ", bounds[0].name)?;
        bounds[0].ty.fmt(fmt)?;
        write!(fmt, ",\n")?;

        for bound in &bounds[1..] {
            write!(fmt, "      {}: ", bound.name)?;
            bound.ty.fmt(fmt)?;
            write!(fmt, ",\n")?;
        }
    }

    Ok(())
}

// ===== impl Fields =====

impl Fields {
    fn named<T>(&mut self, name: &str, ty: T) -> &mut Self
    where T: Into<Type>,
    {
        match *self {
            Fields::Empty => {
                *self = Fields::Named(vec![Field {
                    name: name.to_string(),
                    ty: ty.into(),
                }]);
            }
            Fields::Named(ref mut fields) => {
                fields.push(Field {
                    name: name.to_string(),
                    ty: ty.into(),
                });
            }
            _ => panic!("field list is named"),
        }

        self
    }

    fn tuple<T>(&mut self, ty: T) -> &mut Self
    where T: Into<Type>,
    {
        match *self {
            Fields::Empty => {
                *self = Fields::Tuple(vec![ty.into()]);
            }
            Fields::Tuple(ref mut fields) => {
                fields.push(ty.into());
            }
            _ => panic!("field list is tuple"),
        }

        self
    }

    fn fmt(&self, fmt: &mut Formatter) -> fmt::Result {
        match *self {
            Fields::Named(ref fields) => {
                assert!(!fields.is_empty());

                fmt.block(|fmt| {
                    for f in fields {
                        write!(fmt, "{}: ", f.name)?;
                        f.ty.fmt(fmt)?;
                        write!(fmt, ",\n")?;
                    }

                    Ok(())
                })?;
            }
            Fields::Tuple(ref tys) => {
                assert!(!tys.is_empty());

                write!(fmt, "(")?;

                for (i, ty) in tys.iter().enumerate() {
                    if i != 0 { write!(fmt, ", ")?; }
                    ty.fmt(fmt)?;
                }

                write!(fmt, ")")?;
            }
            Fields::Empty => {}
        }

        Ok(())
    }
}

// ===== impl Impl =====

impl Impl {
    /// Return a new impl definition
    pub fn new<T>(target: T) -> Self
    where T: Into<Type>,
    {
        Impl {
            target: target.into(),
            generics: vec![],
            impl_trait: None,
            assoc_tys: vec![],
            bounds: vec![],
            fns: vec![],
        }
    }

    pub fn generic(&mut self, name: &str) -> &mut Self {
        self.generics.push(name.to_string());
        self
    }

    pub fn target_generic<T>(&mut self, ty: T) -> &mut Self
    where T: Into<Type>,
    {
        self.target.generic(ty);
        self
    }

    pub fn impl_trait(&mut self, name: &str) -> &mut Self {
        self.impl_trait = Some(name.to_string());
        self
    }

    pub fn associate_type<T>(&mut self, name: &str, ty: T) -> &mut Self
    where T: Into<Type>,
    {
        self.assoc_tys.push(Field {
            name: name.to_string(),
            ty: ty.into(),
        });

        self
    }

    pub fn bound<T>(&mut self, name: &str, ty: T) -> &mut Self
    where T: Into<Type>,
    {
        self.bounds.push(Field {
            name: name.to_string(),
            ty: ty.into(),
        });
        self
    }

    /// Define a new function
    pub fn function(&mut self, name: &str) -> &mut Function {
        self.push_function(Function::new(name));
        self.fns.last_mut().unwrap()
    }

    /// Push a function definition
    pub fn push_function(&mut self, func: Function) -> &mut Self {
        self.fns.push(func);
        self
    }

    pub fn fmt(&self, fmt: &mut Formatter) -> fmt::Result {
        write!(fmt, "impl")?;
        fmt_generics(&self.generics[..], fmt)?;

        if let Some(ref t) = self.impl_trait {
            write!(fmt, " {} for", t)?;
        }

        write!(fmt, " ")?;
        self.target.fmt(fmt)?;

        fmt_bounds(&self.bounds, fmt)?;

        fmt.block(|fmt| {
            // format associated types
            if !self.assoc_tys.is_empty() {
                for ty in &self.assoc_tys {
                    write!(fmt, "type {} = ", ty.name)?;
                    ty.ty.fmt(fmt)?;
                    write!(fmt, ";\n")?;
                }
            }

            for (i, func) in self.fns.iter().enumerate() {
                if i != 0 || !self.assoc_tys.is_empty() { write!(fmt, "\n")?; }

                func.fmt(fmt)?;
            }

            Ok(())
        })
    }
}

// ===== impl Import =====

impl Import {
    pub fn new(path: &str, ty: &str) -> Self {
        Import {
            line: format!("{}::{}", path, ty),
            vis: None,
        }
    }

    pub fn vis(&mut self, vis: &str) -> &mut Self {
        self.vis = Some(vis.to_string());
        self
    }
}

// ===== impl Func =====

impl Function {
    pub fn new(name: &str) -> Self {
        Function {
            name: name.to_string(),
            docs: None,
            vis: None,
            generics: vec![],
            arg_self: None,
            args: vec![],
            ret: None,
            bounds: vec![],
            body: vec![],
        }
    }

    pub fn docs(&mut self, docs: &str) -> &mut Self {
        self.docs = Some(Docs::new(docs));
        self
    }

    pub fn vis(&mut self, vis: &str) -> &mut Self {
        self.vis = Some(vis.to_string());
        self
    }

    pub fn generic(&mut self, name: &str) -> &mut Self {
        self.generics.push(name.to_string());
        self
    }

    pub fn arg_self(&mut self) -> &mut Self {
        self.arg_self = Some("self".to_string());
        self
    }

    pub fn arg_ref_self(&mut self) -> &mut Self {
        self.arg_self = Some("&self".to_string());
        self
    }

    pub fn arg_mut_self(&mut self) -> &mut Self {
        self.arg_self = Some("&mut self".to_string());
        self
    }

    pub fn arg<T>(&mut self, name: &str, ty: T) -> &mut Self
    where T: Into<Type>,
    {
        self.args.push(Field {
            name: name.to_string(),
            ty: ty.into(),
        });

        self
    }

    pub fn ret<T>(&mut self, ty: T) -> &mut Self
    where T: Into<Type>,
    {
        self.ret = Some(ty.into());
        self
    }

    pub fn bound<T>(&mut self, name: &str, ty: T) -> &mut Self
    where T: Into<Type>,
    {
        self.bounds.push(Field {
            name: name.to_string(),
            ty: ty.into(),
        });
        self
    }

    pub fn line<T>(&mut self, line: T) -> &mut Self
    where T: ToString,
    {
        self.body.push(Body::String(line.to_string()));
        self
    }

    pub fn block(&mut self, block: Block) -> &mut Self {
        self.body.push(Body::Block(block));
        self
    }

    pub fn fmt(&self, fmt: &mut Formatter) -> fmt::Result {
        if let Some(ref docs) = self.docs {
            docs.fmt(fmt)?;
        }

        if let Some(ref vis) = self.vis {
            write!(fmt, "{} ", vis)?;
        }

        write!(fmt, "fn {}", self.name)?;
        fmt_generics(&self.generics, fmt)?;

        write!(fmt, "(")?;

        if let Some(ref s) = self.arg_self {
            write!(fmt, "{}", s)?;
        }

        for (i, arg) in self.args.iter().enumerate() {
            if i != 0 || self.arg_self.is_some() {
                write!(fmt, ", ")?;
            }

            write!(fmt, "{}: ", arg.name)?;
            arg.ty.fmt(fmt)?;
        }

        write!(fmt, ")")?;

        if let Some(ref ret) = self.ret {
            write!(fmt, " -> ")?;
            ret.fmt(fmt)?;
        }

        fmt_bounds(&self.bounds, fmt)?;

        fmt.block(|fmt| {
            for b in &self.body {
                b.fmt(fmt)?;
            }

            Ok(())
        })
    }
}

// ===== impl Block =====

impl Block {
    pub fn new(before: &str) -> Self {
        Block {
            before: Some(before.to_string()),
            after: None,
            body: vec![],
        }
    }

    pub fn line<T>(&mut self, line: T) -> &mut Self
    where T: ToString,
    {
        self.body.push(Body::String(line.to_string()));
        self
    }

    pub fn block(&mut self, block: Block) -> &mut Self {
        self.body.push(Body::Block(block));
        self
    }

    pub fn after(&mut self, after: &str) -> &mut Self {
        self.after = Some(after.to_string());
        self
    }

    pub fn fmt(&self, fmt: &mut Formatter) -> fmt::Result {
        if let Some(ref before) = self.before {
            write!(fmt, "{}", before)?;
        }

        // Inlined `Formatter::fmt`

        if !fmt.is_start_of_line() {
            write!(fmt, " ")?;
        }

        write!(fmt, "{{\n")?;

        fmt.indent(|fmt| {
            for b in &self.body {
                b.fmt(fmt)?;
            }

            Ok(())
        })?;

        write!(fmt, "}}")?;

        if let Some(ref after) = self.after {
            write!(fmt, "{}", after)?;
        }

        write!(fmt, "\n")?;
        Ok(())
    }
}

// ===== impl Body =====

impl Body {
    pub fn fmt(&self, fmt: &mut Formatter) -> fmt::Result {
        match *self {
            Body::String(ref s) => {
                write!(fmt, "{}\n", s)
            }
            Body::Block(ref b) => {
                b.fmt(fmt)
            }
        }
    }
}

// ===== impl Docs =====

impl Docs {
    fn new(docs: &str) -> Self {
        Docs { docs: docs.to_string() }
    }

    fn fmt(&self, fmt: &mut Formatter) -> fmt::Result {
        for line in self.docs.lines() {
            write!(fmt, "/// {}\n", line)?;
        }

        Ok(())
    }
}

// ===== impl Formatter =====

impl<'a> Formatter<'a> {
    pub fn new(dst: &'a mut String) -> Self {
        Formatter {
            dst,
            spaces: 0,
            indent: DEFAULT_INDENT,
        }
    }

    fn block<F>(&mut self, f: F) -> fmt::Result
    where F: FnOnce(&mut Self) -> fmt::Result
    {
        if !self.is_start_of_line() {
            write!(self, " ")?;
        }

        write!(self, "{{\n")?;
        self.indent(f)?;
        write!(self, "}}\n")?;
        Ok(())
    }

    /// Call the given function with the indentation level incremented by one.
    fn indent<F, R>(&mut self, f: F) -> R
    where F: FnOnce(&mut Self) -> R
    {
        self.spaces += self.indent;
        let ret = f(self);
        self.spaces -= self.indent;
        ret
    }

    fn is_start_of_line(&self) -> bool {
        self.dst.is_empty() ||
            self.dst.as_bytes().last() == Some(&b'\n')
    }

    fn push_spaces(&mut self) {
        for _ in 0..self.spaces {
            self.dst.push_str(" ");
        }
    }
}

impl<'a> fmt::Write for Formatter<'a> {
    fn write_str(&mut self, s: &str) -> fmt::Result {
        let mut first = true;
        let mut should_indent = self.is_start_of_line();

        for line in s.lines() {
            if !first {
                self.dst.push_str("\n");
            }

            first = false;

            let do_indent = should_indent &&
                !line.is_empty() &&
                line.as_bytes()[0] != b'\n';

            if do_indent {
                self.push_spaces();
            }

            // If this loops again, then we just wrote a new line
            should_indent = true;

            self.dst.push_str(line);
        }

        if s.as_bytes().last() == Some(&b'\n') {
            self.dst.push_str("\n");
        }

        Ok(())
    }
}
