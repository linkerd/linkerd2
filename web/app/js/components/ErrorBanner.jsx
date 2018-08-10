import _ from 'lodash';
import { apiErrorPropType } from './util/ApiHelpers.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import { Col, Row } from 'antd';

class ErrorMessage extends React.Component {
  static defaultProps = {
    message: {
      status: null,
      statusText: "An error occured",
      url: "",
      error: ""
    },
    onHideMessage: _.noop
  }

  static propTypes = {
    message: apiErrorPropType,
    onHideMessage: PropTypes.func
  }

  constructor(props) {
    super(props);
    this.hideMessage = this.hideMessage.bind(this);
    this.state = {
      visible: true
    };
  }

  componentWillReceiveProps(newProps) {
    if (!_.isEmpty(newProps.message)) {
      this.setState({ visible :true });
    }
  }

  hideMessage() {
    this.props.onHideMessage();
    this.setState({
      visible: false
    });
  }

  render() {
    const { statusText, error, url, status } = this.props.message;
    return !this.state.visible ? null : (
      <Row gutter={0}>
        <div className="error-message-container">
          <Col span={20}>
            { !status && !statusText ? null : <div>{status} {statusText}</div> }
            { !error ? null : <div>{error}</div> }
            { !url ? null : <div>{url}</div> }
          </Col>
          <Col span={4}>
            <div className="dismiss" onClick={this.hideMessage} role="presentation">Dismiss X</div>
          </Col>
        </div>
      </Row>
    );
  }
}

export default ErrorMessage;
