use codegen;
use prost_build;

#[allow(unused)]
use std::ascii::AsciiExt;
use std::fmt;

fn super_import(ty: &str) -> (String, &str) {
    let mut v: Vec<&str> = ty.split("::").collect();
    v.insert(0, "super");
    let last = v.pop().unwrap_or(ty);
    (v.join("::"), last)
}

fn unqualified(ty: &str) -> &str {
    ty.rsplit("::").next().unwrap_or(ty)
}

/// Generates service code
pub struct ServiceGenerator;

impl ServiceGenerator {
    fn define(&self, service: &prost_build::Service) -> codegen::Scope {
        // Name of the support module. This is the module that will contain all
        // the extra types for the service to avoid potential name conflicts
        // with other services.
        let support_name = service.name.to_ascii_lowercase();

        // Create support module for the service
        let mut support = codegen::Module::new(&support_name);
        support.vis("pub")
            .import("::tower_grpc::codegen::server", "*")
            .import("super", &service.name)
            ;

        // Create a structure for the service
        let mut service_struct = codegen::Struct::new(&service.name);
        service_struct
            .vis("pub")
            .derive("Debug")
            ;

        // A fully not implemented service
        let mut service_not_implemented_ty = codegen::Type::new(&service.name);

        // Create a service implementation for the service struct
        let mut service_impl = codegen::Impl::new(&service.name);
        service_impl.impl_trait("tower::Service")
            .associate_type("Request", "http::Request<tower_h2::RecvBody>")
            .associate_type("Error", "h2::Error")
            // `Response` and `Future` associated type comes later!
            ;

        let mut service_call_block = codegen::Block::new("match request.uri().path()");

        // Create a clone impl for the service struct
        let mut clone_impl = codegen::Impl::new(&service.name);
        clone_impl.impl_trait("Clone")
            ;

        let mut clone_block = codegen::Block::new(&service.name);

        // A `NewService` type. This is a wrapper around the service struct, but
        // also adds builder functions to help define the service.
        let mut new_service = codegen::Struct::new("NewService");
        new_service
            .vis("pub")
            .derive("Debug")
            ;

        let mut new_service_builder_impl = codegen::Impl::new("NewService");

        let mut new_service_trait_impl = codegen::Impl::new("NewService");
        new_service_trait_impl
            .impl_trait("tower::NewService")
            .associate_type("Request", "http::Request<tower_h2::RecvBody>")
            .associate_type("Error", "h2::Error")
            .associate_type("InitError", "h2::Error")
            .associate_type("Future", "futures::FutureResult<Self::Service, Self::Error>")
            // `Response` and `Service` associated type comes later!
            ;

        // A fully not implemented `NewService`
        let mut new_service_not_implemented_ty = codegen::Type::new("NewService");

        let mut new_block = codegen::Block::new(&format!("inner: {}", service.name));

        // Create a response future as an enumeration of the service methods
        // response futures.
        let mut response_fut = codegen::Struct::new("ResponseFuture");
        response_fut
            .vis("pub")
            ;

        let mut response_impl = codegen::Impl::new("ResponseFuture");
        response_impl.impl_trait("futures::Future")
            .associate_type("Error", "h2::Error")
            // `Item` associated type comes later
            ;

        let mut response_debug_impl = codegen::Impl::new("ResponseFuture");
        response_debug_impl.impl_trait("fmt::Debug")
            .function("fmt")
            .arg_ref_self()
            .arg("fmt", "&mut fmt::Formatter")
            .ret("fmt::Result")
            .line("write!(fmt, \"ResponseFuture\")")
            ;

        let mut response_block = codegen::Block::new(&format!("match self.kind"));

        // Create a response body type as an enumeration of the service methods
        // response bodies.
        let mut response_body = codegen::Struct::new("ResponseBody");
        response_body
            .vis("pub")
            ;

        let mut response_body_body_imp = codegen::Impl::new("ResponseBody");
        response_body_body_imp
            .impl_trait("tower_h2::Body")
            .associate_type("Data", "bytes::Bytes")
            ;

        let mut response_body_debug_impl = codegen::Impl::new("ResponseBody");
        response_body_debug_impl.impl_trait("fmt::Debug")
            .function("fmt")
            .arg_ref_self()
            .arg("fmt", "&mut fmt::Formatter")
            .ret("fmt::Result")
            .line("write!(fmt, \"ResponseBody\")")
            ;
            ;

        let mut is_end_stream_block = codegen::Block::new("match self.kind");
        let mut poll_data_block = codegen::Block::new("match self.kind");
        let mut poll_trailers_block = codegen::Block::new("match self.kind");

        // Create the `Kind` enum. This is by the various types above in order
        // to contain the inner types for each of the possible service methods.
        let mut kind_enum = codegen::Enum::new("Kind");
        kind_enum
            .vis("pub(super)")
            .derive("Debug")
            ;

        for method in &service.methods {
            // ===== service struct =====

            // Push a generic representing the method service
            service_struct.generic(&method.proto_name);

            let service_bound = service_bound_for(method);

            // Bound a bound requiring that the service is of the appropriate
            // type.
            service_struct.bound(&method.proto_name, &service_bound);

            // Push a field to hold the service
            service_struct.field(&method.name, field_type_for(&method));

            let ty = unimplemented_type_for(method);
            service_not_implemented_ty.generic(&ty);
            new_service_not_implemented_ty.generic(&ty);

            // ===== Service impl =====

            service_impl.generic(&method.proto_name);
            service_impl.target_generic(&method.proto_name);
            service_impl.bound(&method.proto_name, &service_bound);

            // The service method path.
            let match_line = format!("\"/{}.{}/{}\" =>",
                               service.package,
                               service.proto_name,
                               method.proto_name);

            // Match the service path
            let mut route_block = codegen::Block::new(&match_line);
            route_block
                .line(&format!("let response = self.{}.call(request);", method.name))
                .line(&format!("{}::ResponseFuture {{ kind: Ok({}(response)) }}", support_name, method.proto_name))
                ;

            service_call_block.block(route_block);

            // ===== Clone impl =====

            clone_impl.generic(&method.proto_name);
            clone_impl.target_generic(&method.proto_name);
            clone_impl.bound(&method.proto_name, &service_bound);

            clone_block.line(format!("{name}: self.{name}.clone(),", name = method.name));

            // ===== NewService =====

            new_service.generic(&method.proto_name);
            new_service.bound(&method.proto_name, &service_bound);

            new_service_builder_impl
                .generic(&method.proto_name)
                .target_generic(&method.proto_name)
                .bound(&method.proto_name, &service_bound)
                ;

            new_service_trait_impl
                .generic(&method.proto_name)
                .target_generic(&method.proto_name)
                .bound(&method.proto_name, &service_bound)
                ;

            new_block.line(&format!("{}: {},", method.name, new_unimplemented_for(&method)));

            // ===== ResponseFuture =====

            // Push a generic on the response future type
            response_fut
                .generic(&method.proto_name)
                .bound(&method.proto_name, &service_bound);

            response_impl
                .generic(&method.proto_name)
                .bound(&method.proto_name, &service_bound)
                .target_generic(&method.proto_name);

            response_debug_impl
                .generic(&method.proto_name)
                .target_generic(&method.proto_name)
                .bound(&method.proto_name, &service_bound);

            let match_line = format!("Ok({}(ref mut fut)) =>", method.proto_name);

            let mut try_ready_block = codegen::Block::new("let response = match fut.poll()?");
            try_ready_block
                .line("futures::Async::Ready(v) => v,")
                .line("futures::Async::NotReady => return Ok(futures::Async::NotReady)")
                .after(";")
                ;

            let mut match_block = codegen::Block::new(&match_line);
            match_block
                .block(try_ready_block)
                .line("let (head, body) = response.into_parts();")
                .line(&format!("let body = ResponseBody {{ kind: Ok({}(body)) }};", method.proto_name))
                .line("let response = http::Response::from_parts(head, body);")
                .line("Ok(response.into())")
                ;

            response_block.block(match_block);

            // ===== ResponseBody =====

            response_body
                .generic(&method.proto_name)
                .bound(&method.proto_name, &service_bound)
                ;

            response_body_body_imp
                .generic(&method.proto_name)
                .target_generic(&method.proto_name)
                .bound(&method.proto_name, &service_bound)
                ;

            response_body_debug_impl
                .generic(&method.proto_name)
                .target_generic(&method.proto_name)
                .bound(&method.proto_name, &service_bound)
                ;

            is_end_stream_block
                .line(&format!("Ok({}(ref v)) => v.is_end_stream(),", method.proto_name));

            poll_data_block
                .line(&format!("Ok({}(ref mut v)) => v.poll_data(),", method.proto_name));

            poll_trailers_block
                .line(&format!("Ok({}(ref mut v)) => v.poll_trailers(),", method.proto_name));

            // ===== Kind =====

            // Push a response kind variant
            kind_enum.generic(&method.proto_name);
            kind_enum.variant(&method.proto_name)
                .tuple(&method.proto_name)
                ;

            // ===== support module =====

            let (input_path, input_type) = super_import(&method.input_type);
            let (output_path, output_type) = super_import(&method.output_type);

            // Import the request and response types
            support.import(&input_path, input_type);
            support.import(&output_path, output_type);

        }

        // ===== service impl =====

        // An impl block that contains the `new_service` function. This block is
        // fixed to `NotImplemented` services for all methods.
        let mut service_struct_new = codegen::Impl::new(service_not_implemented_ty);
        service_struct_new.function("new_service")
            .vis("pub")
            .ret(&new_service_not_implemented_ty.path(&support_name))
            .line(&format!("{}::NewService::new()", &support_name))
            ;

        // ===== Service impl =====

        // Add the Future service associated type
        let mut http_response_ty = codegen::Type::new("http::Response");
        http_response_ty.generic(response_body.ty().path(&support_name));

        service_impl
            .associate_type("Response", &http_response_ty)
            .associate_type("Future", response_fut.ty().path(&support_name));

        service_impl.function("poll_ready")
            .arg_mut_self()
            .ret("futures::Poll<(), Self::Error>")
            .line("Ok(().into())")
            ;

        let mut catch_all_block = codegen::Block::new("_ =>");
        catch_all_block
            .line(&format!("{}::ResponseFuture {{ kind: Err(grpc::Status::UNIMPLEMENTED) }}", support_name));

        service_call_block.block(catch_all_block);

        service_impl.function("call")
            .arg_mut_self()
            .arg("request", "Self::Request")
            .ret("Self::Future")
            .line(&format!("use self::{}::Kind::*;", &support_name))
            .line("")
            .block(service_call_block)
            ;

        // ===== Clone impl =====

        clone_impl.function("clone")
            .arg_ref_self()
            .ret("Self")
            .block(clone_block)
            ;

        // ===== NewService =====

        new_service
            .field("inner", service_struct.ty())
            ;


        // Generate all the builder functions
        for (i, m1) in service.methods.iter().enumerate() {
            let service_bound = service_bound_for(m1);

            let mut ret = codegen::Type::new("NewService");

            let mut build = codegen::Block::new(&format!("inner: {}", &service.name));

            // Push all generics onto the return type
            for (j, m2) in service.methods.iter().enumerate() {
                if i == j {
                    ret.generic("T");
                    build.line(&format!("{}: service,", m2.name));
                } else {
                    ret.generic(&m2.proto_name);
                    build.line(&format!("{name}: self.inner.{name},", name = m2.name));
                }
            }

            new_service_builder_impl.function(&m1.name)
                .vis("pub")
                .generic("T")
                .bound("T", service_bound)
                .arg_self()
                .arg("service", "T")
                .ret(ret)
                .line(new_service_line_for(&m1))
                .line("")
                .block({
                    let mut b = codegen::Block::new("NewService");
                    b.block(build);
                    b
                })
                ;
        }

        let mut http_response_ty = codegen::Type::new("http::Response");
        http_response_ty.generic(response_body.ty());

        new_service_trait_impl
            .associate_type("Response", &http_response_ty)
            .associate_type("Service", service_struct.ty())
            .function("new_service")
            .arg_ref_self()
            .ret("Self::Future")
            .line("futures::ok(self.inner.clone())")
            ;

        let mut new_service_not_implemented_impl = codegen::Impl::new(new_service_not_implemented_ty);
        new_service_not_implemented_impl
            .function("new")
            .vis("pub(super)")
            .ret("Self")
            .block(codegen::Block::new("NewService")
                   .block(new_block).clone())
            ;

        // ===== ResponseFuture =====

        let mut ty = codegen::Type::new("Result");
        ty.generic(response_fut_kind(service));
        ty.generic("grpc::Status");

        response_fut
            .field("pub(super) kind", ty)
            ;

        let mut http_response_ty = codegen::Type::new("http::Response");
        http_response_ty.generic(response_body.ty());

        response_impl
            .associate_type("Item", &http_response_ty)
            ;

        let mut match_block = codegen::Block::new("Err(ref status) =>");
        match_block
            .line("let body = ResponseBody { kind: Err(status.clone()) };")
            .line("Ok(grpc::Response::new(body).into_http().into())")
            ;

        response_block.block(match_block);

        response_impl.function("poll")
            .arg_mut_self()
            .ret("futures::Poll<Self::Item, Self::Error>")
            .line("use self::Kind::*;")
            .line("")
            .block(response_block)
            ;

        // ===== ResponseBody =====

        let mut ty = codegen::Type::new("Result");
        ty.generic(response_body_kind(service));
        ty.generic("grpc::Status");

        response_body.field("kind", ty);

        is_end_stream_block
            .line("Err(_) => true,");

        response_body_body_imp.function("is_end_stream")
            .arg_ref_self()
            .ret("bool")
            .line("use self::Kind::*;")
            .line("")
            .block(is_end_stream_block)
            ;

        poll_data_block
            .line("Err(_) => Ok(None.into()),");

        response_body_body_imp.function("poll_data")
            .arg_mut_self()
            .ret("futures::Poll<Option<Self::Data>, h2::Error>")
            .line("use self::Kind::*;")
            .line("")
            .block(poll_data_block)
            ;

        let mut match_block = codegen::Block::new("Err(ref status) =>");
        match_block
            .line("let mut map = http::HeaderMap::new();")
            .line("map.insert(\"grpc-status\", status.to_header_value());")
            .line("Ok(Some(map).into())")
            ;

        poll_trailers_block.block(match_block);

        response_body_body_imp.function("poll_trailers")
            .arg_mut_self()
            .ret("futures::Poll<Option<http::HeaderMap>, h2::Error>")
            .line("use self::Kind::*;")
            .line("")
            .block(poll_trailers_block)
            ;

        // ===== support module =====

        support
            .vis("pub")
            .import("std", "fmt")
            .push_structure(new_service)
            .push_structure(response_fut)
            .push_structure(response_body)
            .push_enumeration(kind_enum)
            .push_imp(new_service_not_implemented_impl)
            .push_imp(new_service_builder_impl)
            .push_imp(new_service_trait_impl)
            .push_imp(response_impl)
            .push_imp(response_debug_impl)
            .push_imp(response_body_body_imp)
            .push_imp(response_body_debug_impl)
            ;

        // Create scope that contains the generated server code.
        let mut scope = codegen::Scope::new();
        {
        let server = scope.module("server")
            .vis("pub")
            .import("::tower_grpc::codegen::server", "*");

        for method in &service.methods {
            let (input_path, input_type) = super_import(&method.input_type);
            let (output_path, output_type) = super_import(&method.output_type);

            // Import the request and response types
            server.import(&input_path, input_type);
            server.import(&output_path, output_type);
        }

        server.push_structure(service_struct)
            .push_imp(service_struct_new)
            .push_imp(service_impl)
            .push_imp(clone_impl)
            .push_module(support)
            ;
        }

        scope

    }

    pub fn generate(&self, service: &prost_build::Service, buf: &mut String) -> fmt::Result {
        let scope = self.define(service);
        let mut fmt = codegen::Formatter::new(buf);

        scope.fmt(&mut fmt)
    }
}

fn field_type_for(method: &prost_build::Method) -> codegen::Type {
    let ty = match (method.client_streaming, method.server_streaming) {
        (false, false) => {
            format!("grpc::Grpc<grpc::Unary<{}, grpc::Decode<{}>>>",
                    method.proto_name, method.input_type)
        }
        (false, true) => {
            format!("grpc::Grpc<grpc::ServerStreaming<{}, grpc::Decode<{}>>>",
                    method.proto_name, method.input_type)
        }
        (true, false) => {
            format!("grpc::Grpc<grpc::ClientStreaming<{}>>",
                    method.proto_name)
        }
        (true, true) => {
            format!("grpc::Grpc<{}>",
                    method.proto_name)
        }
    };

    codegen::Type::from(ty)
}

fn service_bound_for(method: &prost_build::Method) -> codegen::Type {

    let input_type = unqualified(&method.input_type);
    let output_type = unqualified(&method.output_type);

    let ty = match (method.client_streaming, method.server_streaming) {
        (false, false) => {
            format!("grpc::UnaryService<Request = {}, Response = {}>",
                    input_type, output_type)
        }
        (false, true) => {
            format!("grpc::ServerStreamingService<Request = {}, Response = {}>",
                    input_type, output_type)
        }
        (true, false) => {
            format!("grpc::ClientStreamingService<Request = {input}, RequestStream = grpc::Decode<{input}>, Response = {output}>",
                    input = input_type, output = output_type)
        }
        (true, true) => {
            format!("grpc::GrpcService<Request = {input}, RequestStream = grpc::Decode<{input}>, Response = {output}>",
                    input = input_type, output = output_type)
        }
    };

    codegen::Type::from(ty)
}

fn new_service_line_for(method: &prost_build::Method) -> &'static str {
    match (method.client_streaming, method.server_streaming) {
        (false, false) => {
            "let service = grpc::Grpc::new(grpc::Unary::new(service));"
        }
        (false, true) => {
            "let service = grpc::Grpc::new(grpc::ServerStreaming::new(service));"
        }
        (true, false) => {
            "let service = grpc::Grpc::new(grpc::ClientStreaming::new(service));"
        }
        (true, true) => {
            "let service = grpc::Grpc::new(service);"
        }
    }
}

fn unimplemented_type_for(method: &prost_build::Method) -> codegen::Type {
    let ty = match (method.client_streaming, method.server_streaming) {
        (false, false) => {
            format!("grpc::NotImplemented<{}, {}>",
                    unqualified(&method.input_type), unqualified(&method.output_type))
        }
        (false, true) => {
            format!("grpc::NotImplemented<{}, grpc::unary::Once<{}>>",
                    unqualified(&method.input_type), unqualified(&method.output_type))
        }
        (true, false) => {
            format!("grpc::NotImplemented<grpc::Decode<{}>, {}>",
                    unqualified(&method.input_type), unqualified(&method.output_type))
        }
        (true, true) => {
            format!("grpc::NotImplemented<grpc::Decode<{}>, grpc::unary::Once<{}>>",
                    unqualified(&method.input_type), unqualified(&method.output_type))
        }
    };

    codegen::Type::from(ty)
}

fn new_unimplemented_for(method: &prost_build::Method) -> &'static str {
    match (method.client_streaming, method.server_streaming) {
        (false, false) => {
            "grpc::Grpc::new(grpc::Unary::new(grpc::NotImplemented::new()))"
        }
        (false, true) => {
            "grpc::Grpc::new(grpc::ServerStreaming::new(grpc::NotImplemented::new()))"
        }
        (true, false) => {
            "grpc::Grpc::new(grpc::ClientStreaming::new(grpc::NotImplemented::new()))"
        }
        (true, true) => {
            "grpc::Grpc::new(grpc::NotImplemented::new())"
        }
    }
}

// ===== Here be the crazy types =====

fn response_fut_kind(service: &prost_build::Service) -> String {
    use std::fmt::Write;

    // Handle theempty case...
    if service.methods.is_empty() {
        return "Kind".to_string();
    }

    let mut ret = "Kind<\n".to_string();

    for method in &service.methods {
        match (method.client_streaming, method.server_streaming) {
            (false, false) => {
                write!(&mut ret, "    <grpc::Grpc<grpc::Unary<{}, grpc::Decode<{}>>> as tower::Service>::Future,\n",
                                 method.proto_name, method.input_type).unwrap();
            }
            (false, true) => {
                write!(&mut ret, "    <grpc::Grpc<grpc::ServerStreaming<{}, grpc::Decode<{}>>> as tower::Service>::Future,\n",
                                 method.proto_name, method.input_type).unwrap();
            }
            (true, false) => {
                write!(&mut ret, "    <grpc::Grpc<grpc::ClientStreaming<{}>> as tower::Service>::Future,\n",
                                 method.proto_name).unwrap();
            }
            (true, true) => {
                write!(&mut ret, "    <grpc::Grpc<{}> as tower::Service>::Future,\n",
                                 method.proto_name).unwrap();
            }
        }
    }

    ret.push_str(">");
    ret
}

fn response_body_kind(service: &prost_build::Service) -> String {
    use std::fmt::Write;

    // Handle theempty case...
    if service.methods.is_empty() {
        return "Kind".to_string();
    }

    let mut ret = "Kind<\n".to_string();

    for method in &service.methods {
        match (method.client_streaming, method.server_streaming) {
            (false, false) => {
                write!(&mut ret, "    grpc::Encode<<grpc::Unary<{}, grpc::Decode<{}>> as grpc::GrpcService>::ResponseStream>,\n",
                                 method.proto_name, method.input_type).unwrap();
            }
            (false, true) => {
                write!(&mut ret, "    grpc::Encode<<grpc::ServerStreaming<{}, grpc::Decode<{}>> as grpc::GrpcService>::ResponseStream>,\n",
                                 method.proto_name, method.input_type).unwrap();
            }
            (true, false) => {
                write!(&mut ret, "    grpc::Encode<<grpc::ClientStreaming<{}> as grpc::GrpcService>::ResponseStream>,\n",
                                 method.proto_name).unwrap();
            }
            (true, true) => {
                write!(&mut ret, "    grpc::Encode<<{} as grpc::GrpcService>::ResponseStream>,\n",
                                 method.proto_name).unwrap();
            }
        }
    }

    ret.push_str(">");
    ret
}
