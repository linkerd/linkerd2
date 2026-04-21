import { BrowserRouter } from 'react-router-dom';
import BreadcrumbHeader from './BreadcrumbHeader.jsx';
import React from 'react';
import { mount } from 'enzyme';
import { i18nWrap } from '../../test/testHelpers.jsx';

const loc = {
  pathname: '',
  hash: '',
  pathPrefix: '/path/prefix',
  search: '',
};

describe('Tests for <BreadcrumbHeader>', () => {

  it("renders breadcrumbs for a pathname", () => {

    loc.pathname = "/namespaces/emojivoto/deployments/web";
    const component = mount(
      <BrowserRouter>
        <BreadcrumbHeader
          location={loc}
          pathPrefix="" />
      </BrowserRouter>
    );

    const crumbs = component.find("span");
    expect(crumbs).toHaveLength(3);
  });

  it("renders correct breadcrumb text for top-level pages [Namespaces]", () => {
    loc.pathname = "/namespaces";
    const component = mount(
      <BrowserRouter>
        <BreadcrumbHeader
          location={loc}
          pathPrefix="" />
      </BrowserRouter>
    );
    const crumbs = component.find("span");
    expect(crumbs).toHaveLength(1);
    const crumbText = crumbs.reduce((acc, crumb) => {
      acc+= crumb.text();
      return acc;
    }, "")
    expect(crumbText).toEqual("Namespaces");
  });

  it("renders correct breadcrumb text for top-level pages [Control Plane]",
    () => {
    loc.pathname = "/controlplane";
    const component = mount(i18nWrap(
      <BrowserRouter>
        <BreadcrumbHeader
          location={loc}
          pathPrefix="" />
      </BrowserRouter>
    ));
    const crumbs = component.find("span");
    expect(crumbs).toHaveLength(1);
    const crumbText = crumbs.reduce((acc, crumb) => {
      acc+= crumb.text();
      return acc;
    }, "")
    expect(crumbText).toEqual("Control Plane");
  });

  it(`renders correct breadcrumb text for resource list page
    [Namespace > emojivoto > Deployments`, () => {

    loc.pathname = "/namespaces/emojivoto/deployments";
    const component = mount(
      <BrowserRouter>
        <BreadcrumbHeader
          location={loc}
          pathPrefix="" />
      </BrowserRouter>
    );

    const crumbs = component.find("span");
    expect(crumbs).toHaveLength(3);
    const crumbText = crumbs.reduce((acc, crumb) => {
      acc+= crumb.text();
      return acc;
    }, "")
    expect(crumbText).toEqual("Namespace > emojivoto > Deployments")
  });

  it(`renders correct breadcrumb text for resource detail page
    [Namespace > emojivoto > deployment/web`, () => {

    loc.pathname = "/namespaces/emojivoto/deployments/web";
    const component = mount(
      <BrowserRouter>
        <BreadcrumbHeader
          location={loc}
          pathPrefix="" />
      </BrowserRouter>
    );

    const crumbs = component.find("span");
    expect(crumbs).toHaveLength(3);
    const crumbText = crumbs.reduce((acc, crumb) => {
      acc+= crumb.text();
      return acc;
    }, "")
    expect(crumbText).toEqual("Namespace > emojivoto > deployment/web")

  });
});
