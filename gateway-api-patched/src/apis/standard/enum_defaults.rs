// WARNING: generated file - manual changes will be overriden

use super::httproutes::{
    HTTPRouteRulesBackendRefsFiltersRequestRedirectPathType, HTTPRouteRulesBackendRefsFiltersType,
    HTTPRouteRulesBackendRefsFiltersUrlRewritePathType,
    HTTPRouteRulesFiltersRequestRedirectPathType, HTTPRouteRulesFiltersType,
    HTTPRouteRulesFiltersUrlRewritePathType,
};

use super::grpcroutes::{GRPCRouteRulesBackendRefsFiltersType, GRPCRouteRulesFiltersType};

impl Default for GRPCRouteRulesBackendRefsFiltersType {
    fn default() -> Self {
        GRPCRouteRulesBackendRefsFiltersType::RequestHeaderModifier
    }
}

impl Default for GRPCRouteRulesFiltersType {
    fn default() -> Self {
        GRPCRouteRulesFiltersType::RequestHeaderModifier
    }
}

impl Default for HTTPRouteRulesBackendRefsFiltersRequestRedirectPathType {
    fn default() -> Self {
        HTTPRouteRulesBackendRefsFiltersRequestRedirectPathType::ReplaceFullPath
    }
}

impl Default for HTTPRouteRulesBackendRefsFiltersType {
    fn default() -> Self {
        HTTPRouteRulesBackendRefsFiltersType::RequestHeaderModifier
    }
}

impl Default for HTTPRouteRulesBackendRefsFiltersUrlRewritePathType {
    fn default() -> Self {
        HTTPRouteRulesBackendRefsFiltersUrlRewritePathType::ReplaceFullPath
    }
}

impl Default for HTTPRouteRulesFiltersRequestRedirectPathType {
    fn default() -> Self {
        HTTPRouteRulesFiltersRequestRedirectPathType::ReplaceFullPath
    }
}

impl Default for HTTPRouteRulesFiltersType {
    fn default() -> Self {
        HTTPRouteRulesFiltersType::RequestHeaderModifier
    }
}

impl Default for HTTPRouteRulesFiltersUrlRewritePathType {
    fn default() -> Self {
        HTTPRouteRulesFiltersUrlRewritePathType::ReplaceFullPath
    }
}
