import QueryToCliCmd from './QueryToCliCmd.jsx';
import React from 'react';
import { mount } from 'enzyme';

describe('QueryToCliCmd', () => {
  it('renders a query as a linkerd CLI command', () => {
    let query = {
      "resource": "deploy/controller",
      "namespace": "linkerd",
      "scheme": ""
    }
    let cliQueryDisplayOrder = [
      "namespace",
      "scheme"
    ]

    let component = mount(
      <QueryToCliCmd
        cmdName="routes"
        query={query}
        resource={query.resource}
        displayOrder={cliQueryDisplayOrder} />
    );

    expect(component).toIncludeText("Current Routes query");
    expect(component).toIncludeText("linkerd routes deploy/controller --namespace linkerd");
  });

  it('does not render flags for items that are not populated in the query', () => {
    let query = {
      "resource": "deploy/controller",
      "namespace": "linkerd",
      "scheme": "",
      "maxRps": "",
      "authority": "foo.bar:8080"
    }
    let cliQueryDisplayOrder = [
      "namespace",
      "scheme",
      "maxRps",
      "authority"
    ]

    let component = mount(
      <QueryToCliCmd
        cmdName="tap"
        query={query}
        resource={query.resource}
        displayOrder={cliQueryDisplayOrder} />
    );

    expect(component).toIncludeText("Current Tap query");
    expect(component).toIncludeText("linkerd tap deploy/controller --namespace linkerd --authority foo.bar:8080");
  });

  it('displays the flags in the specified displayOrder', () => {
    let query = {
      "resource_name": "deploy/controller",
      "namespace": "linkerd",
      "scheme": "HTTPS",
      "maxRps": "",
      "toResource": "deploy/prometheus",
      "authority": "foo.bar:8080"
    }
    let cliQueryDisplayOrder = [
      "namespace",
      "toResource",
      "scheme",
      "maxRps",
      "authority"
    ]

    let component = mount(
      <QueryToCliCmd
        cmdName="tap"
        query={query}
        resource={query.resource_name}
        displayOrder={cliQueryDisplayOrder} />
    );

    expect(component).toIncludeText("Current Tap query");
    expect(component).toIncludeText("linkerd tap deploy/controller --namespace linkerd --to deploy/prometheus --scheme HTTPS --authority foo.bar:8080");
  });

  it("doesn't render commands for which a flag is not specified", () => {
      let query = {
        "resource": "deploy/controller",
        "namespace": "linkerd",
        "scheme": "HTTPS"
      }
      let cliQueryDisplayOrder = [
        "namespace",
        "theLimitDoesNotExist",
        "scheme"
      ]

      let component = mount(
        <QueryToCliCmd
          cmdName="routes"
          query={query}
          resource={query.resource}
          displayOrder={cliQueryDisplayOrder} />
      );

      expect(component).toIncludeText("Current Routes query");
      expect(component).toIncludeText("linkerd routes deploy/controller --namespace linkerd --scheme HTTPS");
  });
});
