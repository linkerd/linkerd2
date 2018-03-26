import { Table } from 'antd';

export default class BaseTable extends Table {
  constructor(props) {
    super(props);
    Table.prototype.toggleSortOrder = this.toggleSortOrder;
  }

  toggleSortOrder(order, column) {
    let { sortColumn, sortOrder } = this.state;
    let isSortColumn = this.isSortColumn(column);
    sortOrder = order;
    sortColumn = column;

    const newState = {
      sortOrder,
      sortColumn,
    };

    if (this.getSortOrderColumns().length === 0) {
      this.setState(newState);
    }

    const onChange = this.props.onChange;
    if (onChange) {
      onChange.apply(null, this.prepareParamsArguments({
        ...this.state,
        ...newState,
      }));
    }
  }
}
