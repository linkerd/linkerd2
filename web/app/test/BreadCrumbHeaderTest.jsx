import Adapter from 'enzyme-adapter-react-16';
import {Breadcrumb} from 'antd';
import BreadcrumbHeader from '../js/components/BreadcrumbHeader.jsx';
import { BrowserRouter } from 'react-router-dom';
import {expect} from 'chai';
import React from 'react';
import Enzyme, {mount} from 'enzyme';

Enzyme.configure({adapter: new Adapter()});

const loc = {
  pathname: '',
  hash: '',
  pathPrefix: '',
  search: '',
};

describe('Tests for <BreadcrumbHeader>', () => {

  it("renders breadcrumbs for a pathname", () => {

    loc.pathname = "/namespaces/emojivoto/deployments/web";
    const component = mount(
      <BrowserRouter>
        <BreadcrumbHeader
          location={loc} />
      </BrowserRouter>
    );

    const crumbs = component.find(Breadcrumb.Item);
    expect(crumbs).to.have.length(3);
  });
});
