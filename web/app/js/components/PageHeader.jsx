import _ from 'lodash';
import PropTypes from 'prop-types';
import React from 'react';
import { withContext } from './util/AppContext.jsx';
import { Col, Radio, Row } from 'antd';

class PageHeader extends React.Component {
  static defaultProps = {
    hideButtons: false,
    subHeader: null,
    subHeaderTitle: null,
    subMessage: null,
  }

  static propTypes = {
    api: PropTypes.shape({
      getMetricsWindow: PropTypes.func.isRequired,
      getValidMetricsWindows: PropTypes.func.isRequired,
      setMetricsWindow: PropTypes.func.isRequired,
    }).isRequired,
    header: PropTypes.string.isRequired,
    hideButtons: PropTypes.bool,
    subHeader: PropTypes.string,
    subHeaderTitle: PropTypes.string,
    subMessage: PropTypes.string,
  }

  constructor(props) {
    super(props);
    this.onTimeWindowClick = this.onTimeWindowClick.bind(this);
    this.state = {
      selectedWindow: this.props.api.getMetricsWindow()
    };
  }

  onTimeWindowClick(e) {
    let window = e.target.value;
    this.props.api.setMetricsWindow(window);
    this.setState({selectedWindow: window});
  }

  // varying the time window on the web UI is currently hidden
  renderMetricWindowButtons() {
    if (this.props.hideButtons) {return null;}

    const buttons = _.map(this.props.api.getValidMetricsWindows(), (w, i) => {
      return <Radio.Button key={`metrics-window-btn-${i}`} value={w}>{w}</Radio.Button>;
    });

    return (
      <Col span={6}>
        <Radio.Group
          className="time-window-btns"
          value={this.state.selectedWindow}
          onChange={this.onTimeWindowClick} >
          {buttons}
        </Radio.Group>
      </Col>
    );
  }

  render() {
    const {header, subHeader, subHeaderTitle, subMessage} = this.props;

    return (
      <div className="page-header">
        <Row>
          <Col span={18}>
            {!header ? null : <h1>{header}</h1>}
            {!subHeaderTitle ? null : <div className="subsection-header">{subHeaderTitle}</div>}
            {!subHeader ? null : <h1>{subHeader}</h1>}
          </Col>
          {/* {this.renderMetricWindowButtons()} */}
        </Row>

        {!subMessage ? null : <div>{subMessage}</div>}
      </div>
    );
  }
}

export default withContext(PageHeader);
