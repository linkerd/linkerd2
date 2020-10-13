import QueryToCliCmd from './QueryToCliCmd.jsx';
import React from 'react';
import { mount } from 'enzyme';
import { i18nWrap } from '../../test/testHelpers.jsx';

describe('QueryToCliCmd', () => {
  it('renders a query as a linkerd CLI command', () => {
    let query = {
      "resource": "deploy/controller",
      "namespace": "linkerd",
      "scheme": ""
    }

    let component = mount(i18nWrap(
      <QueryToCliCmd
        cmdName="routes"
        query={query}
        resource={query.resource}
        controllerNamespace="linkerd" />)
    );

    expect(component).toIncludeText("Current Routes query");
    expect(component).toIncludeText("linkerd routes deploy/controller --namespace linkerd");
  });

  it('shows the linkerd namespace if the controller is not in the default namespace', () => {
    let query = {
      "resource": "deploy/controller",
      "namespace": "linkerd"
    }

    let component = mount(i18nWrap(
      <QueryToCliCmd
        cmdName="routes"
        query={query}
        resource={query.resource}
        controllerNamespace="my-linkerd-ns" />)
    );

    expect(component).toIncludeText("Current Routes query");
    expect(component).toIncludeText("linkerd routes deploy/controller --namespace linkerd --linkerd-namespace my-linkerd-ns");
  });

  it('does not render flags for items that are not populated in the query', () => {
    let query = {
      "resource": "deploy/controller",
      "namespace": "linkerd",
      "scheme": "",
      "maxRps": "",
      "authority": "foo.bar:8080"
    }

    let component = mount(i18nWrap(
      <QueryToCliCmd
        cmdName="tap"
        query={query}
        resource={query.resource}
        controllerNamespace="linkerd" />)
    );

    expect(component).toIncludeText("Current Tap query");
    expect(component).toIncludeText("linkerd tap deploy/controller --namespace linkerd --authority foo.bar:8080");
  });

  it('displays the flags in the specified order per cli command', () => {
    let query = {
      "resource": "deploy/controller",
      "namespace": "linkerd",
      "scheme": "HTTPS",
      "maxRps": "",
      "toResource": "deploy/prometheus",
      "authority": "foo.bar:8080"
    }

    let component = mount(i18nWrap(
      <QueryToCliCmd
        cmdName="tap"
        query={query}
        resource={query.resource}
        controllerNamespace="linkerd" />)
    );

    expect(component).toIncludeText("Current Tap query");
    expect(component).toIncludeText("linkerd tap deploy/controller --namespace linkerd --to deploy/prometheus --scheme HTTPS --authority foo.bar:8080");
  });

  it("doesn't render a namespace flag when the resource is a namespace", () => {
    let query = {
      "resource": "namespace/linkerd",
      "namespace": "linkerd"
    }

    let component = mount(i18nWrap(
      <QueryToCliCmd
        cmdName="top"
        query={query}
        resource={query.resource}
        controllerNamespace="linkerd" />)
    );

    expect(component).toIncludeText("Current Top query");
    expect(component).toIncludeText("linkerd top namespace/linkerd");
  });

  it("doesn't render commands for which a flag is not defined", () => {
      let query = {
        "resource": "deploy/controller",
        "namespace": "linkerd",
        "scheme": "HTTPS",
        "theLimitDoesNotExist": 999
      }

      let component = mount(i18nWrap(
        <QueryToCliCmd
          cmdName="tap"
          query={query}
          resource={query.resource}
          controllerNamespace="linkerd" />)
      );

      expect(component).toIncludeText("Current Tap query");
      expect(component).toIncludeText("linkerd tap deploy/controller --namespace linkerd --scheme HTTPS");
  });
});
