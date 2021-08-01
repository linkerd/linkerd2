import _merge from 'lodash/merge';
import ApiHelpers from './util/ApiHelpers.jsx';
import BaseTable from './BaseTable.jsx';
import TableBody from '@material-ui/core/TableBody';
import FilterListIcon from '@material-ui/icons/FilterList';
import TableRow from '@material-ui/core/TableRow';
import { routerWrap, i18nWrap } from '../../test/testHelpers.jsx';
import { mount, shallow } from 'enzyme';
import React from 'react';

describe("Tests for <BaseTable>", () => {
  const defaultProps = {
    api: ApiHelpers(""),
  };
  const tableColumns = [{
    dataIndex: "pods.totalPods",
    title: "Meshed"
  },
  {
    dataIndex: "deployment",
    title: "Name"
  },
  {
    dataIndex: "namespace",
    title: "Namespace"
  }];

  it("renders the table with sample data", () => {

    let extraProps = _merge({}, defaultProps, {
      tableRows: [{
        deployment: "authors",
        namespace: "default",
        key: "default-deployment-authors",
        pods: {totalPods: "1", meshedPods: "1"}
      }],
      tableColumns: tableColumns,
    });

    const component = mount(routerWrap(BaseTable, extraProps));
    const table = component.find("BaseTable");
    expect(table).toBeDefined();
    expect(table.props().tableRows).toHaveLength(1);
    expect(table.props().tableColumns).toHaveLength(3);
    expect(table.find("td")).toHaveLength(3);
    expect(table.find("tr")).toHaveLength(2);
  });

  it("if enableFilter is true, user is shown the filter dialog", () => {

    let extraProps = _merge({}, defaultProps, {
      tableRows: [{
        deployment: "authors",
        namespace: "default",
        key: "default-deployment-authors",
        pods: {totalPods: "1", meshedPods: "1"}
      },
      {
        deployment: "books",
        namespace: "default",
        key: "default-deployment-books",
        pods: {totalPods: "2", meshedPods: "1"}
      }],
      tableColumns: tableColumns,
      enableFilter: true
    });

    const component = mount(routerWrap(BaseTable, extraProps));
    const table = component.find("BaseTable");
    const enableFilter = table.prop("enableFilter");
    const filterIcon = table.find("FilterListIcon");
    expect(enableFilter).toEqual(true);
    expect(filterIcon).toBeDefined();
  });

  it("renders the table without data", () => {
    let extraProps = _merge({}, defaultProps, {
      tableRows: [],
      tableColumns: tableColumns,
    });

    const component = mount(i18nWrap(routerWrap(BaseTable, extraProps)));
    const table = component.find("BaseTable");
    expect(table).toBeDefined();
    const emptyCard = table.find("EmptyCard");
    expect(emptyCard).toBeDefined();
    expect(emptyCard).toHaveLength(1);
  });

  it("if enableFilter is true, user can filter rows by search term", () => {
    let extraProps = _merge({}, defaultProps, {
      tableRows: [{
        deployment: "authors",
        namespace: "default",
        key: "default-deployment-authors",
        pods: {totalPods: "1", meshedPods: "1"}
      },
      {
        deployment: "books",
        namespace: "default",
        key: "default-deployment-books",
        pods: {totalPods: "2", meshedPods: "1"}
      }],
      tableColumns: tableColumns,
      enableFilter: true
    });

    const component = shallow(<BaseTable {...extraProps} />);
    const table = component.dive();
    expect(table.find(TableBody).find(TableRow)).toHaveLength(2);
    const enableFilter = component.prop("enableFilter");
    const filterIcon = table.find(FilterListIcon);
    expect(enableFilter).toEqual(true);
    expect(filterIcon).toHaveLength(1);

    filterIcon.simulate("click");
    setTimeout(() => {
      const input = table.find("input");
      input.simulate("change", {target: {value: "authors"}});
      expect(table.html()).not.toContain('books');
      expect(table.html()).toContain('authors');
      expect(table.find(TableBody).find(TableRow)).toHaveLength(1);
    }, 100);
  });
});
