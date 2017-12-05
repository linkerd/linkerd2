use prost_build;

#[allow(unused)]
use std::ascii::AsciiExt;
use std::fmt::{self, Write};

/// Generates service code
pub struct ServiceGenerator;

struct State {
    mod_levels: usize,
}

// ===== impl ServiceGenerator =====

impl ServiceGenerator {
    pub fn generate(&self, service: &prost_build::Service, buf: &mut String) -> fmt::Result {
        let mut state = State {
            mod_levels: 0,
        };
        state.generate(service, buf)
    }
}

// ===== impl State =====

impl State {
    pub fn generate(&mut self, service: &prost_build::Service, buf: &mut String) -> fmt::Result {
        // Generate code in a client module
        write!(buf, "pub mod client {{\n")?;
        self.mod_levels = 1;

        self.generate_svc(&service, buf)?;

        write!(buf, "\n    pub mod {}_methods {{", service.name.to_ascii_lowercase())?;
        self.mod_levels = 2;

        self.generate_rpcs(&service.methods, buf)?;
        buf.push_str("   }\n"); // end `mod name_methods {`

        buf.push_str("}"); // end `mod client {`

        Ok(())
    }

    /// Generates a generic struct to represent the gRPC service.
    fn generate_svc(&self, service: &prost_build::Service, buf: &mut String) -> fmt::Result {
        // Generic names used to identify concrete service implementations
        // contained by the service client struct.
        let mut rpc_generics = vec![];
        let mut all_generics = vec![];
        let mut struct_fields = vec![];
        let mut new_args = vec![];
        let mut new_fields = vec![];
        let mut where_bounds = vec![];

        for method in &service.methods {
            let rpc_name = format!("{}Rpc", method.proto_name);

            let arg = self.input_name(method);
            let returns = self.svc_returns(method);

            rpc_generics.push(rpc_name.clone());
            all_generics.push(rpc_name.clone());

            struct_fields.push(format!("{}: {},", method.name, rpc_name));

            where_bounds.push(format!("{}: ::tower::Service<\
                Request=::tower_grpc::Request<{}>,\
                Response=::tower_grpc::Response<{}>,\
            >,", rpc_name, arg, returns));

            if method.server_streaming {
                all_generics.push(returns.clone());
                where_bounds.push(format!("{}: ::futures::Stream<Item={}, Error=::tower_grpc::Error<::h2::Error>>,", returns, self.output_name(method)));
            }

            new_args.push(format!("{}: {}", method.name, rpc_name));
            new_fields.push(format!("{},", method.name));

        }

        write!(buf, r##"
    #[derive(Debug)]
    pub struct {name}<{rpc_generics}> {{
        {struct_fields}
    }}

    impl<{all_generics}> {name}<{rpc_generics}>
    where
        {where_bounds}
    {{
        pub fn new({new_args}) -> Self {{
            {name} {{
                {new_fields}
            }}
        }}
"##,
            name=service.name,
            all_generics=all_generics.join(", "),
            rpc_generics=rpc_generics.join(", "),
            where_bounds=where_bounds.join("\n"),
            struct_fields=struct_fields.join("\n        "),
            new_args=new_args.join(", "),
            new_fields=new_fields.join("\n        "),
        )?;

        self.generate_svc_methods(&service, buf)?;

        buf.push_str("    }\n"); // close `impl {name} {`

        Ok(())
    }

    /// Generates the inherent methods on the generic service struct.
    fn generate_svc_methods(&self, service: &prost_build::Service, buf: &mut String) -> fmt::Result {
        for method in &service.methods {
            let path = format!("/{}.{}/{}", service.package, service.proto_name, method.proto_name);
            let svc_returns = self.svc_returns(method);
            let rpc_name = format!("{}Rpc", method.proto_name);

            let (returns, map_fut) = if method.server_streaming {
                let returns = format!(
                    "::tower_grpc::client::Streaming<{rpc_name}::Future, {returns}>",
                    rpc_name=rpc_name,
                    returns=svc_returns,
                );
                (returns, "::tower_grpc::client::Streaming::map_future(fut)")
            } else {
                let returns = format!(
                    "::tower_grpc::client::BodyFuture<{rpc_name}::Future>",
                    rpc_name=rpc_name,
                );
                (returns, "::tower_grpc::client::BodyFuture::new(fut)")
            };

            write!(buf, r##"
        pub fn {method}(&mut self, req: {arg}) -> {returns} {{
            let req = ::tower_grpc::Request::new("{path}", req);
            let fut = self.{method}.call(req);
            {fut}
        }}
"##,
            method=method.name,
            arg=self.input_name(method),
            returns=returns,
            path=path,
            fut=map_fut,
            )?;
        }

        Ok(())
    }

    /// Generates the individual RPCs as `tower::Service`.
    fn generate_rpcs(&self, methods: &[prost_build::Method], buf: &mut String) -> fmt::Result {
        for method in methods {
            let input = self.input_name(method);
            let output = self.output_name(method);
            let call_stream = if method.client_streaming {
                unimplemented!("generate client streaming");
            } else {
                "::tower_grpc::client::codec::Unary"
            };

            let where_bounds = format!(r##"
            C: ::tower_grpc::client::Codec<Encode={input}, Decode={output}>,
            S: ::tower::Service<
                Request=::tower_grpc::Request<
                    {call_stream}<{input}>
                >,
                Response=::tower_grpc::Response<
                    ::tower_grpc::client::codec::DecodingBody<C>
                >,
                Error=::tower_grpc::Error<E>
            >,"##,
                call_stream=call_stream,
                input=input,
                output=output,
            );

            let returns = self.rpc_returns(method);
            let fut = if method.server_streaming {
                "S::Future"
            } else {
                "::tower_grpc::client::Unary<S::Future, C>"
            };
            let call = if method.server_streaming {
                "fut"
            } else {
                "::tower_grpc::client::Unary::map_future(fut)"
            };
            write!(buf, r##"
        #[derive(Debug)]
        pub struct {name}<S> {{
            service: S,
        }}

        impl<S, C, E> {name}<S>
        where{where_bounds}
        {{
            pub fn new(service: S) -> Self {{
                {name} {{
                    service,
                }}
            }}
        }}

        impl<S, C, E> ::tower::Service for {name}<S>
        where{where_bounds}
        {{
            type Request = ::tower_grpc::Request<{input}>;
            type Response = ::tower_grpc::Response<{returns}>;
            type Error = S::Error;
            type Future = {fut};

            fn poll_ready(&mut self) -> ::futures::Poll<(), Self::Error> {{
                ::tower::Service::poll_ready(&mut self.service)
            }}

            fn call(&mut self, req: Self::Request) -> Self::Future {{
                let fut = ::tower::Service::call(&mut self.service, {req});
                {call}
            }}
        }}
"##,
                name=method.proto_name,
                where_bounds=where_bounds,
                input=input,
                returns=returns,
                fut=fut,
                req=if method.client_streaming { "req" } else { "req.into_unary()" },
                call=call,
            )?;
        }

        Ok(())
    }

    fn supers(&self) -> String {
        (0..self.mod_levels)
            .map(|_| "super::")
            .collect::<Vec<_>>()
            .concat()
    }

    fn input_name(&self, method: &prost_build::Method) -> String {
        format!("{}{}", self.supers(), method.input_type)
    }

    fn output_name(&self, method: &prost_build::Method) -> String {
        format!("{}{}", self.supers(), method.output_type)
    }

    fn svc_returns(&self, method: &prost_build::Method) -> String {
        if method.server_streaming {
            format!("{}Returns", method.proto_name)
        } else {
            self.output_name(method)
        }
    }

    fn rpc_returns(&self, method: &prost_build::Method) -> String {
        if method.server_streaming {
            "::tower_grpc::client::codec::DecodingBody<C>".into()
        } else {
            self.output_name(method)
        }
    }
}
